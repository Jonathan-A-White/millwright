package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

// handedOffEnv marks an --apply pass that a hand-off started in a unit of its
// own, and names the file that pass creates once it has begun: the parent
// tells a unit that never started (run in place instead) from a pass that
// started and failed (leave with its status).
const handedOffEnv = "MW_APPLY_DETACHED"

// handOff runs the --apply pass a service started in a transient systemd unit
// of its own, outside the service's cgroup, and reports whether it did.
//
// The postern backend starts mw postern inbox --apply as its on-message hook,
// so the pass — and every hands step it runs — lives in postern-backend's
// cgroup, and a step that restarts the backend kills the whole cgroup, mw
// with it, before the ran record and the Ran mail are written. In a unit of
// its own the pass outlives that restart; the process left in the service
// only relays the pass's output and status while it lives. (A root step goes
// through sudo, which starts a session but leaves the cgroup, so it needed
// this too, and has it: the pass that runs it is the one moved.)
//
// It hands off only when INVOCATION_ID says systemd started this process,
// systemd-run is on PATH, and the pass is not already the hand-off's own. When
// no unit could be started it reports false with no error, and the pass runs
// where it is, as it did before.
func handOff(selfPath string, stdout, stderr io.Writer) (bool, error) {
	if os.Getenv("INVOCATION_ID") == "" || os.Getenv(handedOffEnv) != "" {
		return false, nil
	}
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		return false, nil
	}
	started, err := os.CreateTemp("", "mw-apply-started-")
	if err != nil {
		return false, nil
	}
	started.Close()
	defer os.Remove(started.Name())
	os.Remove(started.Name())

	// A unit starts with the manager's environment, not ours: carry ours in.
	args := []string{"--collect", "--quiet", "--wait", "--pipe", "--same-dir",
		"--description=mw postern inbox --apply, out of the service that started it"}
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); !ownToTheService(name) {
			args = append(args, "--setenv="+kv)
		}
	}
	args = append(args, "--setenv="+handedOffEnv+"="+started.Name(), "--", selfPath, "postern", "inbox", "--apply")

	cmd := exec.Command(systemdRun, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	runErr := cmd.Run()
	if _, statErr := os.Stat(started.Name()); statErr != nil {
		return false, nil
	}
	var exited *exec.ExitError
	switch {
	case runErr == nil:
		return true, nil
	case errors.As(runErr, &exited):
		return true, &handedOffExit{code: exited.ExitCode()}
	default:
		return true, runErr
	}
}

// ownToTheService reports whether an environment variable is systemd's own
// word to the service that started this process, which a unit of its own
// gets afresh from systemd and must not be handed a stale copy of.
func ownToTheService(name string) bool {
	switch name {
	case "INVOCATION_ID", "JOURNAL_STREAM", "SYSTEMD_EXEC_PID", "MANAGERPID", "MAINPID",
		"NOTIFY_SOCKET", "WATCHDOG_PID", "WATCHDOG_USEC", "FDSTORE", "MEMORY_PRESSURE_WATCH",
		"MEMORY_PRESSURE_WRITE", "CREDENTIALS_DIRECTORY", "LOGS_DIRECTORY", "STATE_DIRECTORY",
		"RUNTIME_DIRECTORY", "CACHE_DIRECTORY", "CONFIGURATION_DIRECTORY", handedOffEnv, "_", "OLDPWD":
		return true
	}
	return strings.HasPrefix(name, "LISTEN_")
}

// handedOffExit is the status a handed-off pass left with, which mw leaves
// with in turn: the pass has already said why on its own standard error.
type handedOffExit struct{ code int }

func (e *handedOffExit) Error() string {
	return fmt.Sprintf("the apply pass, run in a unit of its own, left with status %d", e.code)
}

// markHandedOff is what the pass a hand-off started does first: create the
// file the parent looks for, and stop a write to a parent that has died from
// killing the pass on SIGPIPE — the parent's death is what the unit is for.
func markHandedOff() {
	name := os.Getenv(handedOffEnv)
	if name == "" {
		return
	}
	signal.Ignore(syscall.SIGPIPE)
	os.WriteFile(name, nil, 0o600)
}

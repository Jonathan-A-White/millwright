package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// detachRig is a stand-in for the host a postern backend's on-message hook
// runs mw on: a systemd-run that starts what it is given in a session of its
// own, as a transient unit's cgroup is out of the service's, and an mw that
// records what its --apply pass did in a "ran" file the way a hands step's
// ran record is.
type detachRig struct {
	dir, ran, out, pgid string
	tmp                 string // TMPDIR of the mw the service started, where a stray marker would land
	env                 []string
}

func newDetachRig(t *testing.T, step string) *detachRig {
	t.Helper()
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("the stand-in for systemd-run needs setsid")
	}
	dir := t.TempDir()
	r := &detachRig{dir: dir, ran: filepath.Join(dir, "ran"), out: filepath.Join(dir, "out"), pgid: filepath.Join(dir, "pgid")}
	systemdRun := `#!/bin/sh
# the service's environment is not the unit's: only what --setenv names crosses
while [ $# -gt 0 ]; do
  case "$1" in
    --setenv=*) export "${1#--setenv=}";;
    --) shift; break;;
  esac
  shift
done
exec setsid -w "$@"
`
	// the stand-in mw: it says it started, runs the step, and records the
	// step's exit and what it printed, as mw's ran record and mail do
	mw := fmt.Sprintf(`#!/bin/sh
echo started > "$MW_APPLY_DETACHED"
( %s ) > %q 2>&1
code=$?
sleep 1
echo "at=1 exit=$code host=laptop $(cat %q)" > %q
cat %q
exit $code
`, step, r.out, r.out, r.ran, r.out)
	r.tmp = filepath.Join(dir, "tmp")
	if err := os.Mkdir(r.tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"systemd-run": systemdRun, "mw": mw} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r.env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"TMPDIR="+r.tmp, "INVOCATION_ID=1234", "MW_TEST_HANDOFF="+filepath.Join(dir, "mw"),
		"MW_APPLY_DETACHED=")
	return r
}

// ranWithin waits for the ran record and returns it.
func (r *detachRig) ranWithin(t *testing.T, limit time.Duration) string {
	t.Helper()
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if raw, err := os.ReadFile(r.ran); err == nil {
			return string(raw)
		}
	}
	t.Fatalf("no ran record within %s", limit)
	return ""
}

// runHandOff runs the parent — the mw the service started — as a process
// group leader of its own, and returns what it printed and its exit status.
func (r *detachRig) runHandOff(t *testing.T) (string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(r.env, "MW_TEST_PGID_FILE="+r.pgid)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.Output()
	if exited, ok := err.(*exec.ExitError); ok {
		return string(out), exited.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// An apply pass the postern backend's hook started, whose step kills the
// process group of the mw that started it — as systemctl --user restart
// postern-backend kills the service's cgroup — still records how the step
// ran.
func TestApplyPassSurvivesItsStepKillingTheProcessThatStartedIt(t *testing.T) {
	r := newDetachRig(t, "kill -s KILL -- -$(cat $MW_TEST_PGID_FILE); echo restarted")
	out, code := r.runHandOff(t)
	if code != -1 {
		t.Fatalf("expected the step to have killed the mw that started the pass, which left with %d and printed %q", code, out)
	}
	if got := r.ranWithin(t, 10*time.Second); !strings.Contains(got, "exit=0") || !strings.Contains(got, "restarted") {
		t.Fatalf("expected a ran record with the step's exit and output, got %q", got)
	}
}

// strays lists what a hand-off left in the temp directory of the mw that made it.
func (r *detachRig) strays(t *testing.T) []string {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(r.tmp, "mw-apply-started-*"))
	if err != nil {
		t.Fatal(err)
	}
	return left
}

// The file a hand-off makes to learn that its unit started is never left in the
// temp directory: not when the pass ends in order, and not when the mw that made
// it is killed while the pass lives on and writes to it (mw-0jx70bj).
func TestHandOffLeavesNoMarkerFileInTheTempDirectory(t *testing.T) {
	for name, step := range map[string]string{
		"the parent killed by the step": "kill -s KILL -- -$(cat $MW_TEST_PGID_FILE); echo restarted",
		"the pass ending in order":      "echo applied",
	} {
		t.Run(name, func(t *testing.T) {
			r := newDetachRig(t, step)
			r.runHandOff(t)
			r.ranWithin(t, 10*time.Second)
			time.Sleep(200 * time.Millisecond) // the pass's last writes
			if left := r.strays(t); len(left) != 0 {
				t.Fatalf("expected no mw-apply-started file left in %s, found %v", r.tmp, left)
			}
		})
	}
}

// A step that kills nothing runs as it always did: its output reaches whoever
// ran mw, its exit is mw's, and its ran record is written.
func TestApplyPassOfANormalStepIsUnchangedByTheHandOff(t *testing.T) {
	r := newDetachRig(t, "echo applied; exit 3")
	out, code := r.runHandOff(t)
	if code != 3 {
		t.Errorf("expected the pass's own exit 3, got %d", code)
	}
	if !strings.Contains(out, "applied") {
		t.Errorf("expected the step's output on the caller's standard output, got %q", out)
	}
	if got := r.ranWithin(t, 10*time.Second); !strings.Contains(got, "exit=3") {
		t.Errorf("expected the ran record to say exit 3, got %q", got)
	}
}

// Where systemd-run cannot start a unit, the pass runs where it is, as before.
func TestApplyPassRunsInPlaceWhenNoUnitCanBeStarted(t *testing.T) {
	r := newDetachRig(t, "true")
	if err := os.WriteFile(filepath.Join(r.dir, "systemd-run"), []byte("#!/bin/sh\necho 'Failed to connect to bus' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := r.runHandOff(t)
	if code != 0 || !strings.Contains(out, "in place") {
		t.Fatalf("expected the pass to run in place, got exit %d and %q", code, out)
	}
}

// handOffAsTheServiceStartedIt is the test binary run as the mw a service
// started: it notes its process group where the step can find it, hands the
// pass off to selfPath, and leaves as mw's main would.
func handOffAsTheServiceStartedIt(selfPath string) {
	os.WriteFile(os.Getenv("MW_TEST_PGID_FILE"), []byte(fmt.Sprint(syscall.Getpgrp())), 0o600)
	handled, err := handOff(selfPath, os.Stdout, os.Stderr)
	switch {
	case err != nil:
		os.Exit(exitStatus(err))
	case !handled:
		fmt.Println("in place")
	}
	os.Exit(0)
}

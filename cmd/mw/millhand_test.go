package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// privateTmux is a tmux server of this test's own, holding the factory's seats
// session with one window in it, and the name of the socket MW_TMUX_SOCKET is
// pointed at. Nothing here can reach the default server, which holds the
// person's windows.
func privateTmux(t *testing.T, window string) string {
	t.Helper()
	return privateTmuxRunning(t, window, "sleep 60")
}

// privateTmuxRunning is privateTmux with the window running the command given.
func privateTmuxRunning(t *testing.T, window, command string) string {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not on PATH")
	}
	socket := fmt.Sprintf("mw-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	t.Setenv("TMUX", "")
	t.Setenv(TmuxSocketEnv, socket)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		_ = os.Remove(socketPath(socket))
	})
	if out, err := exec.Command("tmux", "-L", socket, "new-session", "-d", "-s", "mw-seats", "-n", window, command).CombinedOutput(); err != nil {
		t.Fatalf("starting a tmux server of this test's own: %v: %s", err, out)
	}
	return socket
}

// TestPrivateTmuxRemovesItsSocketFileWhenTheTestEnds guards against the
// helper leaving a dead socket file behind once its server is killed: tmux
// does not remove the file itself on kill-server, only the test's cleanup can.
func TestPrivateTmuxRemovesItsSocketFileWhenTheTestEnds(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not on PATH")
	}
	var path string
	t.Run("inner", func(t *testing.T) {
		socket := privateTmux(t, "scratch")
		path = socketPath(socket)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected the private tmux server's socket file to exist while the test runs, got %v", err)
		}
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected the socket file %s to be removed once the test ended, got %v", path, err)
	}
}

// socketPath is where tmux puts the socket of a server named with -L, so that
// a test can check whether its own socket file is still there.
func socketPath(socket string) string {
	dir := os.Getenv("TMUX_TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), socket)
}

func millhandCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"laptop\"\n")
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"millhand"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestMillhandFindingItselfUpStartsNothingAndLeavesWithFive(t *testing.T) {
	socket := privateTmux(t, "millhand-2026-09-19-03")

	out, err := millhandCmd(t, "--wake", "routine", "--reason", "the timer ticked")
	if err == nil || !strings.Contains(err.Error(), "already up in the window millhand-2026-09-19-03") {
		t.Fatalf("expected a refusal naming the window that is up, got %q, %v", out, err)
	}
	if got := exitCode(err); got != 5 {
		t.Fatalf("expected mw to leave with 5, got %d", got)
	}

	listed, listErr := exec.Command("tmux", "-L", socket, "list-windows", "-t", "mw-seats", "-F", "#{window_name}").Output()
	if listErr != nil {
		t.Fatalf("listing the windows: %v", listErr)
	}
	if got := strings.TrimSpace(string(listed)); got != "millhand-2026-09-19-03" {
		t.Errorf("expected nothing to have been opened beside the window that was up, got %q", got)
	}
}

func TestMillhandRefusesAKindOfWakeItDoesNotKnow(t *testing.T) {
	privateTmux(t, "scratch")

	_, err := millhandCmd(t, "--wake", "nightly")
	if err == nil || !strings.Contains(err.Error(), `"nightly" is not a kind of wake`) {
		t.Fatalf("expected the wake to be refused, got %v", err)
	}
	if got := exitCode(err); got != 1 {
		t.Errorf("expected a plain failure, got status %d", got)
	}
}

func TestMillhandTakesNoArguments(t *testing.T) {
	if _, err := millhandCmd(t, "routine"); err == nil {
		t.Fatal("expected a stray argument to be refused: the kind of wake is --wake")
	}
}

func TestMillhandTickFindingTheMillhandUpSaysSoLogsItAndLeavesWithZero(t *testing.T) {
	privateTmux(t, "millhand-2026-09-19-03")

	out, err := millhandCmd(t, "tick")
	if err != nil {
		t.Fatalf("expected a tick that found the Millhand up to succeed, got %q, %v", out, err)
	}
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "already up (millhand-2026-09-19-03)") {
		t.Fatalf("expected one line saying the Millhand is already up, got %q", out)
	}

	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		t.Fatal(homeErr)
	}
	logged, readErr := os.ReadFile(filepath.Join(home, MillhandTickStateDir, "log"))
	if readErr != nil {
		t.Fatalf("expected the tick to log its line: %v", readErr)
	}
	if strings.TrimSpace(string(logged)) != strings.TrimSpace(out) {
		t.Errorf("expected the log to hold the line that was printed, got %q and %q", logged, out)
	}
}

func TestMillhandTickDryRunLeavesNothingInTheLog(t *testing.T) {
	privateTmux(t, "millhand-2026-09-19-03")

	if out, err := millhandCmd(t, "tick", "--dry-run"); err != nil || !strings.Contains(out, "already up") {
		t.Fatalf("expected a dry run to find the Millhand up, got %q, %v", out, err)
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, MillhandTickStateDir)); !os.IsNotExist(err) {
		t.Errorf("expected a dry run to write nothing, stat says %v", err)
	}
}

func TestMillhandTickDryRunSaysAnIdleMillhandWithNoHandoffWouldBeRestarted(t *testing.T) {
	// An empty prompt of Claude Code's, and no claude: the pane only shows it.
	socket := privateTmuxRunning(t, "millhand-test", "printf '\\342\\235\\257 \\n'; sleep 60")
	t.Setenv("MW_TICK_RECHECK_SECONDS", "0")

	// Stand-ins for bd and git, so that the sync and the mail the tick goes on
	// to reach fail here and reach nothing real.
	stand := t.TempDir()
	for _, name := range []string{"bd", "git"} {
		if err := os.WriteFile(filepath.Join(stand, name), []byte("#!/bin/sh\necho stand-in >&2\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", stand+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The pane is read once the shell has printed the prompt.
	deadline := time.Now().Add(5 * time.Second)
	for {
		screen, _ := exec.Command("tmux", "-L", socket, "capture-pane", "-p", "-t", "mw-seats:millhand-test").Output()
		if strings.Contains(string(screen), "\u276f") || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	out, err := millhandCmd(t, "tick", "--dry-run")
	if !strings.Contains(out, "would be restarted: up but idle since") || !strings.Contains(out, "no handoff") {
		t.Fatalf("expected the dry run to say the idle Millhand would be restarted, got %q, %v", out, err)
	}
	listed, listErr := exec.Command("tmux", "-L", socket, "list-windows", "-t", "mw-seats", "-F", "#{window_name}").Output()
	if listErr != nil {
		t.Fatalf("listing the windows: %v", listErr)
	}
	if got := strings.TrimSpace(string(listed)); got != "millhand-test" {
		t.Errorf("expected a dry run to close and open nothing, got the windows %q", got)
	}
}

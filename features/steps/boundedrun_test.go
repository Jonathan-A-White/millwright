package steps

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mw-gq6.1697: a command line a scenario starts is waited for within a bound,
// and what is left running of it when the scenario is done with it is ended.
// vault_leftovers.feature once hung 18 minutes on a launch line whose fifo
// reader had no writer, and the package's 20 minute timeout refused a landing.

func pidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func pidIn(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(file); err == nil && strings.HasSuffix(string(raw), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatalf("the pid file holds %q: %v", raw, err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never held a pid", file)
	return 0
}

func waitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d is still running", pid)
}

func TestRunBoundedReportsAHungChildAndKillsIt(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	started := time.Now()
	// The shell waits on a child that never ends, as a launch line waits on
	// a fifo reader that has no writer.
	out, err := runBounded(`the session ends as its launch line runs it`, 300*time.Millisecond,
		"sh", "-c", "echo said-this; sleep 30 & echo $! > "+pidFile+"; wait")
	if took := time.Since(started); took > 10*time.Second {
		t.Fatalf("a hung child was waited for %s, not reported within its 300ms bound", took)
	}
	if err == nil {
		t.Fatalf("expected the hung child to be reported, got no error (output %q)", out)
	}
	for _, want := range []string{`the session ends as its launch line runs it`, "300ms", "said-this"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to say %q, got: %v", want, err)
		}
	}
	waitDead(t, pidIn(t, pidFile))
}

func TestRunBoundedKillsWhatTheLineLeavesRunning(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	// The shell ends at once and leaves a child behind, holding its output.
	out, err := runBounded("a line that leaves a child", 30*time.Second,
		"sh", "-c", "echo gone-ahead; sleep 30 & echo $! > "+pidFile)
	if err != nil {
		t.Fatalf("a line that ended well was refused: %v", err)
	}
	if string(out) != "gone-ahead\n" {
		t.Errorf("expected the line's output, got %q", out)
	}
	waitDead(t, pidIn(t, pidFile))
}

func TestRunBoundedReportsAFailingLineWithItsOutput(t *testing.T) {
	out, err := runBounded("a failing line", 30*time.Second, "sh", "-c", "echo why >&2; exit 3")
	var timedOut boundError
	if err == nil || errors.As(err, &timedOut) {
		t.Fatalf("expected the line's own failure, got %v", err)
	}
	if !strings.Contains(string(out), "why") {
		t.Errorf("expected the failing line's output, got %q", out)
	}
}

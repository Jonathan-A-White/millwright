package contrib_test

// Every test here that runs a script or a program does it under a deadline, so
// a child that never exits fails its test in seconds, with what it printed,
// instead of holding the package until go test's own timeout (mw-gq6.700: a
// mail-wait child ran the real `mw events wait` and three gates were refused
// at 10 and 20 minutes).

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	// scriptDeadline is how long a script a contrib test runs may take. The
	// slowest (the scorer's and the bootstrap's own tests) end well inside it.
	scriptDeadline = 60 * time.Second
	// pipeGrace is how long, after the deadline kills the child, a grandchild
	// that still holds its output pipe is waited for before the pipe is cut.
	pipeGrace = 2 * time.Second
)

// command is exec.Command for a test: killed at scriptDeadline, and its output
// pipes cut pipeGrace later, so CombinedOutput and Run return with what was
// printed rather than waiting on a grandchild.
func command(t testing.TB, name string, args ...string) *exec.Cmd {
	t.Helper()
	return commandWithin(t, scriptDeadline, name, args...)
}

// commandWithin is command with a deadline of the caller's choosing.
func commandWithin(t testing.TB, deadline time.Duration, name string, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = pipeGrace
	return cmd
}

func TestACommandWhoseChildHangsFailsAtItsDeadlineWithItsOutput(t *testing.T) {
	// The child prints, then leaves a grandchild holding the output pipe and
	// sleeps: the two ways a CombinedOutput can wait forever.
	cmd := commandWithin(t, time.Second, "sh", "-c", "echo partial; sleep 300 & sleep 300")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("the hung child was waited for %v, want it cut within seconds", took)
	}
	if err == nil {
		t.Fatal("a child killed at its deadline reported success")
	}
	if !strings.Contains(string(out), "partial") {
		t.Fatalf("the hung child's output was lost: %q", out)
	}
}

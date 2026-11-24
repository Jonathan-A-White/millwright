package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// launchBound is how long a scenario waits for a launch line it runs: the
// lines run here end in well under a second, so a minute is only a limit for
// a line that hangs, and far under the 20 minutes go test gives the package.
const launchBound = time.Minute

// boundError is a command line that was still running when its bound passed,
// and was killed, with what it had said so far (mw-gq6.1697).
type boundError struct {
	step   string
	bound  time.Duration
	output []byte
}

func (e boundError) Error() string {
	return fmt.Sprintf("%s: the command line was still running after its %s bound and was killed with its children; it had said:\n%s",
		e.step, e.bound, e.output)
}

// runBounded runs the command in a process group of its own and waits for it
// at most bound; step names what the scenario was doing, for the failure. Its
// output goes to a file, not a pipe, so a child the line leaves behind holding
// it cannot keep the wait going. The whole group is killed when the wait ends,
// in time or not: what a scenario starts, the scenario ends. A line still
// running at the bound is a boundError.
func runBounded(step string, bound time.Duration, name string, args ...string) ([]byte, error) {
	return runBoundedIn(step, bound, nil, name, args...)
}

// runBoundedIn is runBounded with a hook to set the command up (its directory
// and environment) before it starts.
func runBoundedIn(step string, bound time.Duration, setup func(*exec.Cmd), name string, args ...string) ([]byte, error) {
	out, err := os.CreateTemp("", "mw-bounded-run-")
	if err != nil {
		return nil, fmt.Errorf("%s: making a file for the command's output: %w", step, err)
	}
	defer os.Remove(out.Name())
	defer out.Close()

	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if setup != nil {
		setup(cmd)
	}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return endGroup(cmd) }
	runErr := cmd.Run()
	endGroup(cmd)

	said, _ := os.ReadFile(out.Name())
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return said, boundError{step: step, bound: bound, output: said}
	}
	return said, runErr
}

// endGroup kills the command's process group: the command and whatever it
// started that has not ended.
func endGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

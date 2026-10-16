// Package shellcmd runs one shell command line in a directory and keeps what
// it printed: the adapter behind application.BootCommands, for the commands a
// seat's boot manifest names. The line is given to sh -c as it is, so it may
// hold what a shell reads — pipes, quotes, $HOME — and runs with mw's own
// environment.
package shellcmd

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultTimeout is how long one command may run when nothing says otherwise.
const DefaultTimeout = 2 * time.Minute

// Runner runs commands, each for at most Timeout; 0 is DefaultTimeout.
type Runner struct {
	Timeout time.Duration
}

var _ application.BootCommands = Runner{}

// New is a Runner whose commands run for at most timeout.
func New(timeout time.Duration) Runner {
	return Runner{Timeout: timeout}
}

// Run implements application.BootCommands: the command line under sh -c in dir,
// its stdout and stderr together as printed. The shell runs in a process group
// of its own, so that a command stopped at its timeout takes with it what it
// started underneath. A command that fails, or runs out of time, comes back with
// what it printed so far and an error saying which.
func (r Runner) Run(ctx context.Context, dir, command string) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("gave up after %s", timeout)
	}
	return string(out), err
}

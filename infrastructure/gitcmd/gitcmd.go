// Package gitcmd builds the git commands every rig and vault adapter runs: one
// place that says how mw's git is kept from hanging.
package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// pipeGrace is how long a git that has ended, or been told to stop, is given for
// its output to close and be read before mw cuts it off and fails the command.
// On a busy host a git that succeeded can take more than a second over that
// (mw-gq6.1610). A stop kills the whole process group, which closes the output
// at once, so a stopped git does not wait this long.
const pipeGrace = 10 * time.Second

// lowSpeedLimit and lowSpeedTime bound how long a git network call may sit
// transferring almost nothing before it gives up: below lowSpeedLimit
// bytes/sec for lowSpeedTime seconds, git fails it itself rather than a sync
// holding a tick while a stalled connection sits there. They are not a config
// knob — every git call gets the same wait, on both hosts.
const (
	lowSpeedLimit = "1000"
	lowSpeedTime  = "60"
)

// Command returns a git command for program, run in dir (the current directory
// when dir is empty), that never prompts and never waits on a stalled
// connection: a sync or a dispatch may run on a timer with nobody at the
// keyboard, and a command waiting for a password would hang there until
// someone noticed.
//
// It runs in a process group of its own, so that a context that ends — mw told
// to stop by a signal, or a tick giving up — takes the whole group with it,
// rather than leaving a fetch or push running behind mw's back.
func Command(ctx context.Context, program, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		"GIT_HTTP_LOW_SPEED_LIMIT="+lowSpeedLimit, "GIT_HTTP_LOW_SPEED_TIME="+lowSpeedTime)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = pipeGrace
	return cmd
}

// Package reaper starts the watcher that closes a finished session's window. It
// is the adapter behind application.ReapArmer: the watch itself is
// application.SeatReap, run by `mw seat reap`, and this package only starts
// that command as a process that outlives the one starting it.
package reaper

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"github.com/Jonathan-A-White/millwright/application"
)

// Armer starts `mw seat reap` detached.
type Armer struct {
	program string
}

// Armer satisfies the port.
var _ application.ReapArmer = (*Armer)(nil)

// New returns an Armer that starts the given program as mw: the path of the mw
// binary that is running, so that the reaper is the same build as the command
// that armed it.
func New(program string) *Armer {
	return &Armer{program: program}
}

// Arm implements application.ReapArmer: `mw seat reap <seat> --window <id>`,
// with --when-idle for idle mode, in a session and process group of its own and
// with no terminal to write to, so that it survives the window it was started
// from — which is the one it will close — and everything else that ends with
// mw. It returns once the process has started; how the watch ends is in the
// seat's reaper log.
//
// The process inherits mw's environment, and with it the tmux server and the
// vault this mw was pointed at.
func (a *Armer) Arm(_ context.Context, arming application.ReapArming) error {
	switch {
	case arming.Seat == "" || arming.Window == "":
		return fmt.Errorf("a reaper watches a seat's window: got seat %q, window %q", arming.Seat, arming.Window)
	case arming.Mode != application.ReapSuccessor && arming.Mode != application.ReapWhenIdle:
		return fmt.Errorf("a reaper is armed in successor or when-idle mode, not %q", arming.Mode)
	}

	args := []string{"seat", "reap", arming.Seat, "--window", arming.Window}
	if arming.Mode == application.ReapWhenIdle {
		args = append(args, "--when-idle")
	}

	// Not exec.CommandContext: the process is meant to outlive the context it
	// was armed in. Its standard streams are left nil, which is the null device.
	cmd := exec.Command(a.program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting the reaper for window %s: %w", arming.Window, err)
	}
	// Nobody waits for it: let go of it rather than leave it a zombie of this
	// process for as long as this process lives.
	return cmd.Process.Release()
}

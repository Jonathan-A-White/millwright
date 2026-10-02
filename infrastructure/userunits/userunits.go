// Package userunits starts the systemd user units the factory's jobs are:
// what the follower does to run a dispatch pass, the Millhand's tick or the
// mail-notify job when an event springs it, so each runs as its own unit, with
// its own limits and outside the follower's control group (the Builder
// sessions' tmux server lives in the dispatch unit's).
package userunits

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Systemctl runs the user manager's systemctl: Bin, or systemctl on the PATH.
type Systemctl struct {
	Bin string
}

func (s Systemctl) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := s.Bin
	if bin == "" {
		bin = "systemctl"
	}
	return exec.CommandContext(ctx, bin, append([]string{"--user"}, args...)...).CombinedOutput()
}

// Installed is whether the user manager knows unit.
func (s Systemctl) Installed(ctx context.Context, unit string) bool {
	_, err := s.run(ctx, "cat", unit)
	return err == nil
}

// Start starts unit and waits for it to end: a oneshot unit's start returns
// when its run does, and fails when the run did. A unit that is running
// already is waited for, not started twice.
func (s Systemctl) Start(ctx context.Context, unit string) error {
	out, err := s.run(ctx, "start", unit)
	if err != nil {
		if said := strings.Join(strings.Fields(string(out)), " "); said != "" {
			return fmt.Errorf("starting %s: %v: %s", unit, err, said)
		}
		return fmt.Errorf("starting %s: %w", unit, err)
	}
	return nil
}

// TryRestart restarts unit when it is running, so that it runs the binary just
// built. A unit the user manager does not know, or that is not running, is left
// as it is and is not an error: a host runs only some of the factory's units.
func (s Systemctl) TryRestart(ctx context.Context, unit string) (bool, error) {
	if !s.Installed(ctx, unit) {
		return false, nil
	}
	// is-active exits non-zero for a unit that is not running.
	if _, err := s.run(ctx, "is-active", "--quiet", unit); err != nil {
		return false, nil
	}
	out, err := s.run(ctx, "try-restart", unit)
	if err != nil {
		if said := strings.Join(strings.Fields(string(out)), " "); said != "" {
			return false, fmt.Errorf("restarting %s: %v: %s", unit, err, said)
		}
		return false, fmt.Errorf("restarting %s: %w", unit, err)
	}
	return true, nil
}

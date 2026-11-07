// Package userunits starts the systemd user units the factory's jobs are:
// what the follower does to run a dispatch pass, the Millhand's tick or the
// mail-notify job when an event springs it, so each runs as its own unit, with
// its own limits and outside the follower's control group (the Builder
// sessions' tmux server lives in the dispatch unit's).
package userunits

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Systemctl runs the user manager's systemctl: Bin, or systemctl on the PATH.
// With SystemScope it runs the system manager's instead (no --user), which is
// where a root-seat host's units live (mw-gq6.1035).
type Systemctl struct {
	Bin string
	// SystemScope asks the system manager, not the user manager.
	SystemScope bool
	// Root says whether this process runs as root, for ReloadStaleSystem. Nil
	// reads the effective user id.
	Root func() bool
}

func (s Systemctl) isRoot() bool {
	if s.Root != nil {
		return s.Root()
	}
	return os.Geteuid() == 0
}

func (s Systemctl) run(ctx context.Context, args ...string) ([]byte, error) {
	bin := s.Bin
	if bin == "" {
		bin = "systemctl"
	}
	if !s.SystemScope {
		args = append([]string{"--user"}, args...)
	}
	return exec.CommandContext(ctx, bin, args...).CombinedOutput()
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

// StaleUnits names every mw-* user unit the user manager has loaded whose file
// has changed on disk since (`NeedDaemonReload=yes`): the installed units are
// links into the rig's checkout, so a landing that edits one changes the file
// the manager still holds the old reading of (mw-gq6.565). A host with no such
// unit, or none loaded, has none.
func (s Systemctl) StaleUnits(ctx context.Context) ([]string, error) {
	listed, err := s.run(ctx, "list-units", "--all", "--plain", "--no-legend", "--full", "mw-*")
	if err != nil {
		return nil, fmt.Errorf("listing the mw user units: %w", err)
	}
	var names []string
	for _, line := range strings.Split(string(listed), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && strings.HasPrefix(fields[0], "mw-") {
			names = append(names, fields[0])
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	shown, err := s.run(ctx, append([]string{"show", "-p", "Id", "-p", "NeedDaemonReload"}, names...)...)
	if err != nil {
		return nil, fmt.Errorf("asking whether the mw user units need a reload: %w", err)
	}
	var stale []string
	for _, block := range strings.Split(string(shown), "\n\n") {
		var id, need string
		for _, line := range strings.Split(block, "\n") {
			key, value, found := strings.Cut(strings.TrimSpace(line), "=")
			switch {
			case !found:
			case key == "Id":
				id = strings.TrimSpace(value)
			case key == "NeedDaemonReload":
				need = strings.TrimSpace(value)
			}
		}
		if id != "" && need == "yes" {
			stale = append(stale, id)
		}
	}
	return stale, nil
}

// ReloadStale runs `systemctl --user daemon-reload` (no --user for SystemScope) when StaleUnits names any
// unit, and not otherwise, and returns the units it found stale. A reload that
// fails is an error carrying what systemctl said, with the units still named.
func (s Systemctl) ReloadStale(ctx context.Context) ([]string, error) {
	stale, err := s.StaleUnits(ctx)
	if err != nil || len(stale) == 0 {
		return nil, err
	}
	out, err := s.run(ctx, "daemon-reload")
	if err != nil {
		if said := strings.Join(strings.Fields(string(out)), " "); said != "" {
			return stale, fmt.Errorf("daemon-reload: %v: %s", err, said)
		}
		return stale, fmt.Errorf("daemon-reload: %w", err)
	}
	return stale, nil
}

// ReloadStaleSystem is ReloadStale for the system manager, and only when this
// process runs as root: a root-seat host's mw-* system units (mw-seat-tmux.service,
// mw-doctor.*) are links into the checkout like the user units, and a landing
// that edits one leaves the system manager on the old reading until a
// `systemctl daemon-reload` (mw-gq6.1035). A non-root process asks nothing and
// returns none.
func (s Systemctl) ReloadStaleSystem(ctx context.Context) ([]string, error) {
	if !s.isRoot() {
		return nil, nil
	}
	s.SystemScope = true
	return s.ReloadStale(ctx)
}

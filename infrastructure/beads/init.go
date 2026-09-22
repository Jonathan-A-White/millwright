package beads

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/Jonathan-A-White/millwright/application"
)

// Gateway satisfies the port a vault's beads database is made through.
var _ application.TrackerBirth = (*Gateway)(nil)

// InitTracker implements application.TrackerBirth: it makes a beads database
// with the prefix in the Gateway's directory, which must already be a git
// repository. It is the one place mw runs `bd init`, and it does not go through
// run: `bd -C <dir>` refuses a directory that has no database yet, so bd is
// started in the directory instead. Git hooks and the agent-instruction files
// bd offers are skipped: the vault has its own CLAUDE.md, and its hooks are not
// mw's to install.
func (g *Gateway) InitTracker(ctx context.Context, prefix string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	args := []string{"init", "-p", prefix, "--non-interactive", "--role", "maintainer", "--skip-agents", "--skip-hooks", "-q"}
	cmd := exec.CommandContext(ctx, g.program, args...)
	cmd.Dir = g.vault
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if said := said(out.Bytes(), errs.Bytes()); said != "" {
			return fmt.Errorf("%s init in %s: %w: %s", g.program, g.vault, err, said)
		}
		return fmt.Errorf("%s init in %s: %w", g.program, g.vault, err)
	}
	return nil
}

// BootstrapTracker implements application.TrackerBirth: it picks up the beads
// database already in the Gateway's directory, cloned from wherever this
// host's vault came from — bd bootstrap, never bd init and never bd migrate.
// Like InitTracker it does not go through run: `bd -C <dir>` refuses a
// directory that has no database yet, so bd is started in the directory
// instead.
func (g *Gateway) BootstrapTracker(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	cmd := exec.CommandContext(ctx, g.program, "bootstrap", "--yes")
	cmd.Dir = g.vault
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if said := said(out.Bytes(), errs.Bytes()); said != "" {
			return fmt.Errorf("%s bootstrap in %s: %w: %s", g.program, g.vault, err, said)
		}
		return fmt.Errorf("%s bootstrap in %s: %w", g.program, g.vault, err)
	}
	return nil
}

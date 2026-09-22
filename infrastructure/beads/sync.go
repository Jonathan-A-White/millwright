package beads

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/netfault"
)

// Gateway also brings the one beads database level with the other host's, and
// is where a read-only reader of the hosts' notes — mw status — reads them.
var (
	_ application.TrackerSync  = (*Gateway)(nil)
	_ application.TrackerNotes = (*Gateway)(nil)
)

// NoteMissing is what bd says when a key is not in its key-value store — it
// exits 1 and prints `<key> (not set)`. That is not a failure here: a host that
// has never synced simply has no note yet. Both wordings are read, because
// which one bd uses has changed before.
var NoteMissing = []string{"not set", "not found"}

// Sync implements application.TrackerSync: one `bd sync` — pull, look for
// conflicts, recompute what is blocked, push with bd's own bounded retries.
//
// bd's exit code is surfaced as it was, in a *application.SyncHalt. Nothing is
// retried here: a conflict (bd 2) does sometimes come right within seconds,
// but the one retry it gets for that is application.Sync's, run by calling
// this method again — not this gateway's. bd 4 (a working set only a person
// can clear) never comes right on its own, and bd 3 is bd reporting that it
// has already spent its retries. Nothing is resolved here either: a conflict
// in the factory's one database is the Governor's to look at.
func (g *Gateway) Sync(ctx context.Context) error {
	out, errs, err := g.run(ctx, "sync")
	if err == nil {
		return nil
	}

	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		// bd never ran, or was killed: there is no exit code to be faithful to.
		return fmt.Errorf("%s sync in %s: %w", g.program, g.vault, err)
	}
	told := said(out, errs)
	halt := &application.SyncHalt{Code: exited.ExitCode(), Said: firstLine(told)}
	// A bd that could not resolve the remote's name is still bd's halt, with its
	// exit code, underneath; the wrapper is what lets a dispatch wait for the
	// network to come back.
	if line, unresolved := netfault.NameNotResolved(told); unresolved {
		return &application.NameNotResolved{Said: line, Err: halt}
	}
	return halt
}

// Note implements application.TrackerSync. A key that is not there comes back
// empty, because a host the other one has never heard from is a fact, not a
// failure.
func (g *Gateway) Note(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("a note needs a key")
	}
	out, err := g.call(ctx, "kv", "get", key)
	if err != nil {
		said := strings.ToLower(err.Error())
		for _, missing := range NoteMissing {
			if strings.Contains(said, missing) {
				return "", nil
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SetNote implements application.TrackerSync.
func (g *Gateway) SetNote(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	_, err := g.call(ctx, "kv", "set", key, value)
	return err
}

// firstLine is the first thing bd said, which is the sentence worth repeating;
// the rest is the hint underneath it.
func firstLine(said string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(said), "\n")
	return strings.TrimSpace(line)
}

// ClearNote implements application.TrackerSync. A key that is not there is
// already cleared, so bd's word that it is missing is not a failure.
func (g *Gateway) ClearNote(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	if _, err := g.call(ctx, "kv", "clear", key); err != nil {
		said := strings.ToLower(err.Error())
		for _, missing := range NoteMissing {
			if strings.Contains(said, missing) {
				return nil
			}
		}
		return err
	}
	return nil
}

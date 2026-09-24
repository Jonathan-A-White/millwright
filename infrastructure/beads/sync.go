package beads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/netfault"
)

// Gateway also brings the one beads database level with the other host's, and
// is where a read-only reader of the hosts' notes — mw status — reads them.
var (
	_ application.TrackerSync  = (*Gateway)(nil)
	_ application.TrackerNotes = (*Gateway)(nil)
	_ application.DoctorNotes  = (*Gateway)(nil)
)

// beadsDir is where bd keeps everything about one vault's database — the
// working database, its auto-commit history, and its auto-backups — relative
// to the vault directory a Gateway runs bd in.
const beadsDir = ".beads"

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

// NotesWithPrefix implements application.DoctorNotes. bd's own `kv list` has
// no prefix filter, so this reads the whole table and filters it here; a
// non-string value (bd's own "schema_version" among them) is not a note and
// is skipped rather than failing the read.
func (g *Gateway) NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	out, err := g.call(ctx, "kv", "list", "--json")
	if err != nil {
		return nil, err
	}
	var all map[string]any
	if err := json.Unmarshal(out, &all); err != nil {
		return nil, fmt.Errorf("parsing bd kv list --json: %w", err)
	}
	found := map[string]string{}
	for key, value := range all {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if text, ok := value.(string); ok {
			found[key] = text
		}
	}
	return found, nil
}

// GC implements application.TrackerSync: asks bd to reclaim the disk space
// its own auto-commit history piles up — its compact and Dolt GC phases —
// then repacks the git-remote-cache bare clones Dolt keeps beside the
// database, the one thing bd's own gc never touches: every `bd dolt
// push`/pull leaves a new full pack there. Decay, which deletes issues closed
// a long time ago, is skipped: choosing to delete tracked work is a person's
// call, not something a sync makes on a timer.
//
// A repack that fails is reported the same way a failed `bd gc` itself would
// be: a plain error from GC, never a reason bd's own collection is undone.
func (g *Gateway) GC(ctx context.Context) error {
	if _, err := g.call(ctx, "gc", "--skip-decay", "--force"); err != nil {
		return err
	}
	return g.repackRemoteCaches(ctx)
}

// Size implements application.TrackerNotes: every byte this host's beads
// database occupies on disk under .beads in the vault — the working
// database, its auto-commit history, and whatever bd itself keeps there, its
// auto-backups included. It is host-local: an adapter never sizes another
// host's disk, so mw status only ever calls this for the host it runs on. A
// vault that has not been bd-initialised yet has no .beads at all, which
// sizes as 0 rather than a failure: there is nothing there yet, not an error.
func (g *Gateway) Size(_ context.Context) (int64, error) {
	root := filepath.Join(g.vault, beadsDir)
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("sizing the beads database in %s: %w", g.vault, err)
	}
	return total, nil
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

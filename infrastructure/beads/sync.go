package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/netfault"
)

// Gateway also brings the one beads database level with the other host's, and
// is where a read-only reader of the hosts' notes — mw status — reads them.
var (
	_ application.TrackerSync       = (*Gateway)(nil)
	_ application.TrackerRepackSaid = (*Gateway)(nil)
	_ application.TrackerHistory    = (*Gateway)(nil)
	_ application.TrackerFlush      = (*Gateway)(nil)
	_ application.TrackerNotes      = (*Gateway)(nil)
	_ application.DoctorNotes       = (*Gateway)(nil)
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
// conflicts, recompute what is blocked, push with bd's own bounded retries —
// then a check of the git-remote-cache clones Dolt keeps beside the database,
// repacking whichever has grown past remoteCacheRepackThreshold packs. That
// check runs on every sync, not only the once-a-day GC: the cache can gain a
// full pack on every `bd dolt push`/pull, faster than a daily collection can
// keep up. A repack that fails there is never a reason to fail the sync
// itself — the pull, conflict check and push already got through — so its
// error is not returned: it is kept for TakeRepackErr, for the sync's line
// to say (mw-gq6.619).
//
// bd's exit code is surfaced as it was, in a *application.SyncHalt. Nothing is
// retried here: a conflict (bd 2) does sometimes come right within seconds,
// but the one retry it gets for that is application.Sync's, run by calling
// this method again — not this gateway's. bd 4 (a working set only a person
// can clear) never comes right on its own, and bd 3 is bd reporting that it
// has already spent its retries. Nothing is resolved here either: a conflict
// in the factory's one database is the Governor's to look at.
func (g *Gateway) Sync(ctx context.Context) error {
	// In bd's batch mode the pending writes are in no commit, so in no push or
	// backup either until someone flushes (mw-gq6.508).
	if _, err := g.Flush(ctx, time.Now()); err != nil {
		return fmt.Errorf("flushing the beads database before bd sync: %w", err)
	}
	out, errs, err := g.run(ctx, "sync")
	if err == nil {
		g.keepRepackErr(g.repackCrowdedRemoteCaches(ctx))
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
	if g.notes != nil {
		if kept, ok := g.notes.get(key, g.clock()); ok {
			return kept, nil
		}
	}
	if value, ok := g.noteOverConnection(ctx, key); ok {
		g.keepNote(key, value)
		return value, nil
	}
	out, err := g.call(ctx, "kv", "get", key)
	if err != nil {
		said := strings.ToLower(err.Error())
		for _, missing := range NoteMissing {
			if strings.Contains(said, missing) {
				g.keepNote(key, "")
				return "", nil
			}
		}
		return "", err
	}
	value := strings.TrimSpace(string(out))
	g.keepNote(key, value)
	return value, nil
}

// keepNote remembers key's value, when this Gateway keeps notes.
func (g *Gateway) keepNote(key, value string) {
	if g.notes != nil {
		g.notes.put(key, value, g.clock())
	}
}

// NotesWithPrefix implements application.DoctorNotes. bd's own `kv list` has
// no prefix filter, so this reads the whole table and filters it here; a
// non-string value (bd's own "schema_version" among them) is not a note and
// is skipped rather than failing the read.
func (g *Gateway) NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error) {
	if found, ok := g.notesOverConnection(ctx, prefix); ok {
		return found, nil
	}
	out, err := g.call(ctx, "kv", "list", "--json")
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return map[string]string{}, nil
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

// GC implements application.TrackerSync: reclaims the disk space the beads
// database's own auto-commit history piles up — its compact and Dolt GC phases —
// then repacks the git-remote-cache bare clones Dolt keeps beside the
// database, the one thing bd's own gc never touches: every `bd dolt
// push`/pull leaves a new full pack there. Decay, which deletes issues closed
// a long time ago, is skipped: choosing to delete tracked work is a person's
// call, not something a sync makes on a timer.
//
// A collection the server refused is a failure (collect), and so is a repack
// that fails: a plain error from GC, never a reason the collection is undone.
func (g *Gateway) GC(ctx context.Context) error {
	if err := g.collect(ctx, false); err != nil {
		return err
	}
	return g.repackRemoteCaches(ctx)
}

// deniedWords are how a Dolt server says its user may not call a procedure:
// "command denied to user 'beads'@'%'". bd gc exits 0 on that answer
// (mw-gq6.629), so every collection reads what was said as well as the exit.
const deniedWords = "command denied"

// collect reclaims the disk space of the beads database, fully when full is
// set (a full collection reaches the history an earlier one moved to the old
// generation). On a host that serves its beads the server's user may not call
// DOLT_GC, so the Dolt command line runs it as the local Dolt user in the data
// folder; elsewhere bd gc does. An answer that the command was denied is an
// error on either, whatever the exit status.
func (g *Gateway) collect(ctx context.Context, full bool) error {
	dataDir, database, served, err := g.servedDatabase()
	if err != nil {
		return err
	}
	if served {
		procedure := "CALL DOLT_GC()"
		if full {
			procedure = "CALL DOLT_GC('--full')"
		}
		return g.runDolt(ctx, dataDir, "--use-db", database, "sql", "-q", procedure)
	}
	args := []string{"gc", "--skip-decay", "--force"}
	if full {
		args = append(args, "--full")
	}
	return g.callProcedure(ctx, args...)
}

// callProcedure is call for a command that runs a privileged procedure on the
// server: an answer of "command denied" is a failure although bd exited 0.
func (g *Gateway) callProcedure(ctx context.Context, args ...string) error {
	out, errs, err := g.run(ctx, args...)
	if err != nil {
		return g.failed(err, out, errs, args)
	}
	if told := said(out, errs); strings.Contains(strings.ToLower(told), deniedWords) {
		return fmt.Errorf("%s %s: %s", g.program, strings.Join(args, " "), told)
	}
	return nil
}

// servedDatabase says whether this host serves its beads (.beads/dolt holds the
// data folder, as it does for serverModeRefusal) and, when it does, the data
// folder and the name of the database in it, from .beads/metadata.json.
func (g *Gateway) servedDatabase() (dataDir, database string, served bool, err error) {
	dataDir = filepath.Join(g.vault, beadsDir, "dolt")
	if info, statErr := os.Stat(dataDir); statErr != nil || !info.IsDir() {
		return "", "", false, nil
	}
	raw, readErr := os.ReadFile(filepath.Join(g.vault, beadsDir, "metadata.json"))
	var meta struct {
		Database string `json:"dolt_database"`
	}
	if readErr != nil || json.Unmarshal(raw, &meta) != nil || meta.Database == "" {
		return "", "", false, fmt.Errorf("collecting the beads in %s: no dolt_database named in %s", g.vault, filepath.Join(beadsDir, "metadata.json"))
	}
	return dataDir, meta.Database, true, nil
}

// runDolt runs the Dolt command line in dir, as the local user: dolt reaches a
// server running on the data folder as __dolt_local_user__, which the beads
// user's refusals do not bind. Only one runs at a time beside the bd calls, as
// the database takes a single writer. Like bd it has a process group of its
// own and no standard input.
func (g *Gateway) runDolt(ctx context.Context, dir string, args ...string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	program := g.dolt
	if program == "" {
		program = "dolt"
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace
	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs
	err := cmd.Run()
	told := strings.TrimSpace(errs.String() + "\n" + out.String())
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("stopped: %w", ctx.Err())
	}
	if err != nil {
		return fmt.Errorf("%s %s in %s: %w: %s", program, strings.Join(args, " "), dir, err, told)
	}
	if strings.Contains(strings.ToLower(told), deniedWords) {
		return fmt.Errorf("%s %s in %s: %s", program, strings.Join(args, " "), dir, told)
	}
	return nil
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

// SetNote implements application.TrackerSync. A note the kept connection reads
// as holding value already is not written again: the read costs no bd, and a
// bd is several new connections to the server, for a note most passes leave as
// it was (mw-gq6.612).
func (g *Gateway) SetNote(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	if held, ok := g.noteOverConnection(ctx, key); ok && held == strings.TrimSpace(value) {
		g.keepNote(key, held)
		return nil
	}
	_, err := g.call(ctx, "kv", "set", key, value)
	if err != nil {
		if g.notes != nil {
			g.notes.forget(key)
		}
		return err
	}
	g.keepNote(key, strings.TrimSpace(value))
	return nil
}

// firstLine is the first thing bd said, which is the sentence worth repeating;
// the rest is the hint underneath it.
func firstLine(said string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(said), "\n")
	return strings.TrimSpace(line)
}

// ClearNote implements application.TrackerSync. A key that is not there is
// already cleared, so bd's word that it is missing is not a failure; and one the
// kept connection reads as not there, or empty, is not cleared again, which
// costs no bd (mw-gq6.612).
func (g *Gateway) ClearNote(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	if held, ok := g.noteOverConnection(ctx, key); ok && held == "" {
		g.keepNote(key, "")
		return nil
	}
	if _, err := g.call(ctx, "kv", "clear", key); err != nil {
		said := strings.ToLower(err.Error())
		for _, missing := range NoteMissing {
			if strings.Contains(said, missing) {
				g.keepNote(key, "")
				return nil
			}
		}
		if g.notes != nil {
			g.notes.forget(key)
		}
		return err
	}
	g.keepNote(key, "")
	return nil
}

// squashMessage is the commit a squash of the history leaves on top of the
// first one.
const squashMessage = "mw: squash the beads history into one commit"

// HistoryCommits implements application.TrackerHistory: Dolt's own count of the
// commits on the branch. bd answers SQL only from a served database, so on an
// embedded one this is an error, and the history is never squashed there.
func (g *Gateway) HistoryCommits(ctx context.Context) (int, error) {
	var rows []struct {
		Commits int `json:"commits"`
	}
	if err := g.query(ctx, "SELECT COUNT(*) AS commits FROM dolt_log", &rows); err != nil {
		return 0, err
	}
	if len(rows) != 1 {
		return 0, fmt.Errorf("counting the beads history: bd sql returned %d rows", len(rows))
	}
	return rows[0].Commits, nil
}

// SquashHistory implements application.TrackerHistory. bd flatten does the same
// with a hard reset of the branch onto a squashed copy of it, which drops any
// write that lands between the copy and the reset; this resets softly onto the
// first commit and commits there, which moves only the branch's head and never
// the working set every write lands in. A write in between commits the whole
// working set onto the first commit itself, and the squash's own commit then
// has nothing left to commit: that is the squash done. Tried on a copy of the
// Laptop's database served by Dolt 2.2.0, twenty squashes under three writers
// lost none of 600 writes (mw-gq6.455).
//
// The remote-tracking branches go next, since each keeps its whole history
// alive: the Laptop's backup left one that held 43,506 commits. A later fetch
// makes them again. Last, a full collection: the history an earlier
// collection moved to the old generation is reclaimed by no other.
func (g *Gateway) SquashHistory(ctx context.Context) error {
	// A flush first, so that batch mode's pending writes are in a commit the
	// squash folds in (mw-gq6.508).
	if _, err := g.Flush(ctx, time.Now()); err != nil {
		return fmt.Errorf("flushing the beads database before the squash: %w", err)
	}
	var first []struct {
		Hash string `json:"commit_hash"`
	}
	if err := g.query(ctx, "SELECT commit_hash FROM dolt_log ORDER BY date ASC LIMIT 1", &first); err != nil {
		return err
	}
	if len(first) != 1 || strings.TrimSpace(first[0].Hash) == "" {
		return fmt.Errorf("squashing the beads history: bd sql named no first commit")
	}
	if err := g.callProcedure(ctx, "sql", fmt.Sprintf("CALL DOLT_RESET('--soft', %s)", sqlQuoted(first[0].Hash))); err != nil {
		return err
	}
	if err := g.callProcedure(ctx, "sql", fmt.Sprintf("CALL DOLT_COMMIT('-Am', '%s')", squashMessage)); err != nil &&
		!strings.Contains(err.Error(), "nothing to commit") {
		return err
	}
	var remotes []struct {
		Name string `json:"name"`
	}
	if err := g.query(ctx, "SELECT name FROM dolt_remote_branches", &remotes); err != nil {
		return err
	}
	for _, remote := range remotes {
		if err := g.callProcedure(ctx, "sql", fmt.Sprintf("CALL DOLT_BRANCH('-D', '-r', %s)", sqlQuoted(remote.Name))); err != nil {
			return err
		}
	}
	if err := g.collect(ctx, true); err != nil {
		return fmt.Errorf("the full collection after the squash: %w", err)
	}
	return nil
}

// query runs one SQL query through bd and reads its JSON rows into rows.
func (g *Gateway) query(ctx context.Context, query string, rows any) error {
	out, err := g.call(ctx, "sql", "--json", query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, rows); err != nil {
		return fmt.Errorf("parsing bd sql --json for %q: %w", query, err)
	}
	return nil
}

// FlushMessage is the commit message of a flush made at the time given.
func FlushMessage(at time.Time) string {
	return "mw flush " + at.UTC().Format(time.RFC3339)
}

// nothingToFlush are the words bd uses for a working set with no change in it.
var nothingToFlush = []string{"nothing to commit", "no changes"}

// Flush implements application.TrackerFlush: one `bd dolt commit`, which
// commits every pending write of every client of the database together. In
// bd's batch mode that is the only commit there is; in its default mode there
// is never anything left to commit. A working set that is clean is not a
// failure, only a flush that committed nothing.
func (g *Gateway) Flush(ctx context.Context, at time.Time) (bool, error) {
	if _, err := g.call(ctx, "dolt", "commit", "-m", FlushMessage(at)); err != nil {
		said := strings.ToLower(err.Error())
		for _, clean := range nothingToFlush {
			if strings.Contains(said, clean) {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

// AutoCommit implements application.TrackerFlush: what bd answers to `bd
// config get dolt.auto-commit` here, the environment (BD_DOLT_AUTO_COMMIT)
// and the flag counted as bd counts them.
func (g *Gateway) AutoCommit(ctx context.Context) (string, error) {
	out, err := g.call(ctx, "config", "get", "dolt.auto-commit")
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(string(out))), nil
}

// TakeRepackErr implements application.TrackerRepackSaid: why the repack the
// last successful Sync ended with failed, once. The sync itself did not fail
// for it, but a failure nobody is told of reads as a cache kept in check
// (mw-gq6.619).
func (g *Gateway) TakeRepackErr() error {
	g.repackMu.Lock()
	defer g.repackMu.Unlock()
	err := g.repackErr
	g.repackErr = nil
	return err
}

// keepRepackErr remembers the outcome of the repack a sync just ended with.
func (g *Gateway) keepRepackErr(err error) {
	g.repackMu.Lock()
	defer g.repackMu.Unlock()
	g.repackErr = err
}

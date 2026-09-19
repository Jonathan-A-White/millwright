package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LedgerPattern is the part of the vault both hosts append to and neither one
// edits, and MergeUnion is how git is told to merge it: keep both sides' lines
// rather than call two appends a conflict. Together they are the line the
// vault's .gitattributes carries — LedgerMark.
const (
	LedgerPattern = "seats/*/ledger.md"
	MergeUnion    = "merge=union"
	LedgerMark    = LedgerPattern + " " + MergeUnion
)

// LastSyncKey is where a host records when it was last level with the other
// one. Both hosts read each other's, so the name carries the host: the key for
// the VPS is host.vps.last_sync.
func LastSyncKey(host string) string { return "host." + host + ".last_sync" }

// LastSyncFormat is how a sync time is written down: RFC 3339 in UTC, so that
// two hosts in two time zones compare as text.
const LastSyncFormat = time.RFC3339

// VaultFiles is the port mw keeps the vault's files in step with the other
// host through. One adapter is the vault's git clone on disk.
//
// Nothing here forces anything: a clone that cannot be brought level says so
// and leaves the vault as it found it.
type VaultFiles interface {
	// MarkLedgers makes sure the vault tells git that ledgers are appended to
	// by both hosts and merged by keeping every line — LedgerMark in the
	// vault's .gitattributes. It reports whether it had to write it. A vault
	// already marked is left alone.
	MarkLedgers(ctx context.Context) (bool, error)

	// Uncommitted lists the tracked files changed but not committed. A vault
	// with any of them cannot be rebased onto the other host's work, and
	// committing them is not a sync's job. Files git does not track are not
	// listed: they are in nobody's way.
	Uncommitted(ctx context.Context) ([]string, error)

	// Commit records exactly the paths it is given, each one from the vault's
	// root, under message, and reports which of them there was really something
	// to commit in. Nothing else the vault holds is touched — not a file
	// somebody else changed, not one somebody else staged — and a path that is
	// not there, or that has not changed, is left out rather than refused: a
	// commit with nothing in it is not made at all, and that is not an error.
	//
	// It is how the one thing mw writes in the vault during a story, and the one
	// thing the session may write beside it, stop standing in the way of the
	// next sync. Anything else uncommitted is still somebody's to commit.
	Commit(ctx context.Context, message string, paths []string) ([]string, error)

	// Pull brings the other host's commits in and replays this host's on top,
	// reporting how many came in. It is a plain rebase: nothing is forced, and
	// a rebase that stops on a conflict is undone before the error comes back,
	// so that the vault is left as it was.
	Pull(ctx context.Context) (int, error)

	// Push publishes the commits this host has and the other one does not,
	// reporting how many went out. A clone with nothing to push does not reach
	// the remote at all.
	Push(ctx context.Context) (int, error)
}

// TrackerSync is the port mw brings the one beads database level with the other
// host through, and the small notes the hosts leave each other in it. The beads
// gateway is the adapter; an in-memory one stands in for it in tests.
//
// It is deliberately narrow: mw asks for one synchronisation cycle and reads
// what came back. It never migrates the database, never resolves a conflict and
// never forces a push — the tracker itself decides what it can settle.
type TrackerSync interface {
	// Sync runs one synchronisation cycle against the tracker's remote. A cycle
	// that halted comes back as a *SyncHalt carrying the tracker's own exit
	// code, so that mw can say plainly what stopped and stop too.
	Sync(ctx context.Context) error

	// Note reads one value out of the tracker's key-value store, or "" when the
	// key is not there.
	Note(ctx context.Context, key string) (string, error)

	// SetNote writes one value into the tracker's key-value store.
	SetNote(ctx context.Context, key, value string) error
}

// SyncHalt is a tracker synchronisation that stopped without publishing
// anything. Code is the tracker's own exit code, surfaced as it was: mw neither
// hides it nor interprets it beyond saying, in plain words, what it means.
//
// The codes are beads': 1 an error, 2 a merge conflict it will not resolve
// itself, 3 a push race it has already retried, 4 a working set only a person
// can clear. Nothing here is ever retried by mw — 2 and 4 because no retry can
// help, 3 because the tracker has already spent its own retries.
type SyncHalt struct {
	Code int
	// Said is what the tracker printed, kept so that a person reading the
	// failure sees the tracker's own words after mw's.
	Said string
}

// Error is the plain sentence a person reads when a sync stops, with the
// tracker's own exit code in it and the tracker's own words after it.
func (h *SyncHalt) Error() string {
	message := fmt.Sprintf("%s (bd sync exited %d)", h.reason(), h.Code)
	if said := strings.TrimSpace(h.Said); said != "" {
		return message + "; bd said: " + said
	}
	return message
}

// reason says in plain words what the tracker's exit code means.
func (h *SyncHalt) reason() string {
	switch h.Code {
	case 2:
		return "the beads database has a merge conflict this sync will not resolve for you: " +
			"nothing was pushed, and no later sync will push until it is resolved by hand"
	case 3:
		return "the beads database lost the push race too many times: " +
			"nothing was pushed, and the next sync will try again"
	case 4:
		return "the beads database has a stuck working set that no retry will clear: " +
			"nothing was pushed, and it stays that way until a person clears it by hand"
	default:
		return "the beads database could not be synced"
	}
}

// Transient reports whether trying again later could get past this halt. Only a
// lost push race is transient; a conflict or a stuck working set waits for a
// person, and mw retries neither.
func (h *SyncHalt) Transient() bool { return h.Code == 3 }

// Halted reports whether err is a tracker sync that stopped, and what stopped
// it.
func Halted(err error) (*SyncHalt, bool) {
	var halt *SyncHalt
	return halt, errors.As(err, &halt)
}

// SyncReport is what one sync did. Marked says the vault had to be told that
// ledgers merge by union; Pulled and Pushed count the commits that moved; At is
// the time recorded for this host, and is zero when nothing was recorded.
type SyncReport struct {
	Host   string
	Marked bool
	Pulled int
	Pushed int
	At     time.Time
}

// Quiet reports whether the sync found nothing to do: nothing to mark, nothing
// to pull and nothing to push.
func (r SyncReport) Quiet() bool { return !r.Marked && r.Pulled == 0 && r.Pushed == 0 }

// String is the one line mw prints when a sync finishes.
func (r SyncReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: ", r.Host)
	if r.Quiet() {
		b.WriteString("the vault was already level")
	} else {
		fmt.Fprintf(&b, "pulled %d, pushed %d", r.Pulled, r.Pushed)
		if r.Marked {
			fmt.Fprintf(&b, "; marked %s as %s (commit it, so the other host is covered too)", LedgerPattern, MergeUnion)
		}
	}
	b.WriteString("; beads synced")
	if !r.At.IsZero() {
		fmt.Fprintf(&b, "; level at %s", r.At.UTC().Format(LastSyncFormat))
	}
	return b.String()
}

// Sync brings this host level with the other one: the vault's files, then the
// beads database, then a note of when this host was last level.
//
// The order is the point. The vault's ledgers merge cleanly only once git has
// been told they do, so the mark goes in before anything is pulled. The note is
// written last, because only a sync that finished is one this host was level
// at — which means the note itself reaches the other host on the next sync, one
// cycle later. That is the honest trade: a note written earlier would travel
// sooner and lie whenever the sync went on to fail.
//
// Nothing here is retried and nothing is forced. A sync that cannot finish
// leaves the vault as it found it and hands back the reason.
type Sync struct {
	Vault   VaultFiles
	Tracker TrackerSync
	Host    string

	// Now is the clock, so that a test can pin the time a sync was level at.
	// The zero value reads the real one.
	Now func() time.Time
}

// Run does one sync and reports what moved.
func (s Sync) Run(ctx context.Context) (SyncReport, error) {
	switch {
	case s.Vault == nil || s.Tracker == nil:
		return SyncReport{}, fmt.Errorf("syncing: a sync needs a vault and a work tracker")
	case s.Host == "":
		return SyncReport{}, fmt.Errorf("syncing: which host is this? set MW_HOST, or host in the config file")
	}
	report := SyncReport{Host: s.Host}

	// A vault holding work nobody has committed cannot be rebased onto the
	// other host's, and committing someone else's work is not a sync's job.
	dirty, err := s.Vault.Uncommitted(ctx)
	if err != nil {
		return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
	}
	if len(dirty) > 0 {
		return report, fmt.Errorf("syncing the vault on %s: it holds uncommitted changes to %s: "+
			"commit them and sync again; nothing was pulled and nothing was pushed",
			s.Host, strings.Join(dirty, ", "))
	}

	if report.Marked, err = s.Vault.MarkLedgers(ctx); err != nil {
		return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
	}
	if report.Pulled, err = s.Vault.Pull(ctx); err != nil {
		return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
	}
	if report.Pushed, err = s.Vault.Push(ctx); err != nil {
		return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
	}

	if err := s.Tracker.Sync(ctx); err != nil {
		return report, fmt.Errorf("syncing the beads database on %s: %w", s.Host, err)
	}

	report.At = s.now()
	if err := s.Tracker.SetNote(ctx, LastSyncKey(s.Host), report.At.UTC().Format(LastSyncFormat)); err != nil {
		report.At = time.Time{}
		return report, fmt.Errorf("recording when %s was last level: %w", s.Host, err)
	}
	return report, nil
}

// now is the clock this sync reads.
func (s Sync) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

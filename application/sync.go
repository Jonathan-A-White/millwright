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

// ConflictRetryDelay is how long Sync.Run waits before giving a merge conflict
// (bd exit 2) its one retry, so that a conflict that is about to clear on its
// own has had a moment to do so.
const ConflictRetryDelay = 5 * time.Second

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

	// ClearNote deletes one key from the tracker's key-value store. A key that
	// is not there is already what was asked for, and is not an error.
	ClearNote(ctx context.Context, key string) error
}

// SyncHalt is a tracker synchronisation that stopped without publishing
// anything. Code is the tracker's own exit code, surfaced as it was: mw neither
// hides it nor interprets it beyond saying, in plain words, what it means.
//
// The codes are beads': 1 an error, 2 a merge conflict it will not resolve
// itself, 3 a push race it has already retried, 4 a working set only a person
// can clear. A conflict that looks stuck sometimes clears within seconds, so
// Sync.Run gives exit 2 exactly one retry before declaring the halt; nothing
// else is ever retried by mw — 4 because no retry can help, 3 because the
// tracker has already spent its own retries.
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

// VaultBlockedExit is the status mw leaves with when the one thing in a sync's
// way was work somebody has not committed in the vault. It is a status of its
// own on purpose: 1 is any plain failure and 2, 3 and 4 are beads' own, so a
// timer reading nothing but the number can tell "one forgotten edit is waiting
// for whoever wrote it" from "something is broken".
const VaultBlockedExit = 5

// VaultBlocked is a sync whose vault half could not run because the vault holds
// changes to tracked files that nobody has committed. Committing them belongs
// to whoever wrote them — mw never commits on a seat's behalf — so the vault is
// left exactly as it was found, and the beads half runs anyway: hosts that stay
// level in beads keep working while one edit waits.
//
// It is not a fault, and a timer should not treat it as one: it carries
// VaultBlockedExit, and says what is in the way in one line.
type VaultBlocked struct {
	Host string
	// Files are the tracked files changed but not committed, as the vault
	// reported them.
	Files []string
}

// Error is the one line a person, or a timer's log, reads: which host, which
// files, and what mw did and did not do about them.
func (b *VaultBlocked) Error() string {
	return fmt.Sprintf("%s: the vault holds uncommitted changes to %s, so the vault was left alone: "+
		"nothing was pulled, nothing was pushed, and mw commits nobody's work for them; "+
		"beads were synced all the same, so commit them when you can",
		b.Host, strings.Join(b.Files, ", "))
}

// Blocked reports whether err is a sync whose vault half was blocked by work
// nobody committed, and what was in the way.
func Blocked(err error) (*VaultBlocked, bool) {
	var blocked *VaultBlocked
	return blocked, errors.As(err, &blocked)
}

// ExitStatus is the status mw leaves with when a command reports err: beads'
// own exit code when beads stopped a sync, VaultBlockedExit when nothing was
// wrong but somebody's uncommitted vault work, MillhandUpExit when the Millhand
// was not started because one is up, WatchWakeExit when mw watch calls for a
// wake, NetworkFaultExit when a dispatch waited out the network and it did not
// come back, 1 for anything else, and 0 for nothing wrong at all. cmd/mw leaves with it.
func ExitStatus(err error) int {
	if err == nil {
		return 0
	}
	if halt, stopped := Halted(err); stopped && halt.Code != 0 {
		return halt.Code
	}
	if _, stopped := Blocked(err); stopped {
		return VaultBlockedExit
	}
	if _, up := MillhandIsUp(err); up {
		return MillhandUpExit
	}
	if _, wake := WatchWakes(err); wake {
		return WatchWakeExit
	}
	if _, fault := LocalFault(err); fault {
		return NetworkFaultExit
	}
	return 1
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

	// Blocked names the uncommitted tracked files that kept the vault half from
	// running at all. It is empty for every sync that got to touch the vault.
	Blocked []string

	// Retried is the notice that a merge conflict on the first beads cycle
	// cleared on the one retry, naming what bd said on that first try. Empty
	// when no retry happened.
	Retried string
}

// Quiet reports whether the sync found nothing to do: nothing to mark, nothing
// to pull and nothing to push. A sync the vault blocked is not quiet — it has
// something to say, even though nothing moved.
func (r SyncReport) Quiet() bool {
	return !r.Marked && r.Pulled == 0 && r.Pushed == 0 && len(r.Blocked) == 0
}

// String is the one line mw prints when a sync finishes.
func (r SyncReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: ", r.Host)
	switch {
	case len(r.Blocked) > 0:
		fmt.Fprintf(&b, "the vault was left alone, it holds uncommitted changes to %s", strings.Join(r.Blocked, ", "))
	case r.Quiet():
		b.WriteString("the vault was already level")
	default:
		fmt.Fprintf(&b, "pulled %d, pushed %d", r.Pulled, r.Pushed)
		if r.Marked {
			fmt.Fprintf(&b, "; marked %s as %s (commit it, so the other host is covered too)", LedgerPattern, MergeUnion)
		}
	}
	b.WriteString("; beads synced")
	if r.Retried != "" {
		fmt.Fprintf(&b, "; %s", r.Retried)
	}
	if !r.At.IsZero() {
		fmt.Fprintf(&b, "; level at %s", r.At.UTC().Format(LastSyncFormat))
	}
	return b.String()
}

// Sync brings this host level with the other one: the vault's files, then the
// beads database, carrying a note of when this host was last level.
//
// The order is the point. The vault's ledgers merge cleanly only once git has
// been told they do, so the mark goes in before anything is pulled. The note is
// written once the vault's half is level and before the beads cycle, because
// that cycle is what pushes it: the other host reads it on its next sync, not
// one cycle later. A cycle that halts pushed nothing, so the note is taken back
// out and never says a host was level when it was not.
//
// Nothing here is forced, and the only retry is the beads cycle's own: a merge
// conflict is given one more try before it is declared a halt. A sync that
// cannot finish leaves the vault as it found it and hands back the reason.
//
// One thing stops half of it rather than all of it. A vault holding work nobody
// committed blocks the vault's half only: mw commits nobody's edits, so it
// leaves them alone, syncs beads anyway, and comes back with a *VaultBlocked
// naming the files. That is what lets a sync run on a timer — one forgotten
// ledger edit no longer costs the hosts every later tick — and it is still a
// stop, so nothing that must not run on a stale vault runs after it.
type Sync struct {
	Vault   VaultFiles
	Tracker TrackerSync
	Host    string

	// Ticks are the logs this host's timers keep. Every sync that gets level
	// leaves the counts of them beside the note of when it was, as TicksKey, so
	// that the other host can say how this one's timers are doing without a
	// tunnel to it. A host that keeps no log leaves no note, and a log that
	// cannot be read, or a note that cannot be written, is left out: none of them
	// is the sync's to fail.
	Ticks TickLogs

	// Now is the clock, so that a test can pin the time a sync was level at.
	// The zero value reads the real one.
	Now func() time.Time

	// Sleep waits out the pause before a conflict's one retry, or until ctx
	// ends; a nil Sleep sleeps for real.
	Sleep func(ctx context.Context, d time.Duration) error
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
	// other host's, and committing someone else's work is not a sync's job. So
	// the vault's half is skipped whole — nothing marked, nothing pulled,
	// nothing pushed — and the beads half runs anyway: on a timer, one forgotten
	// edit must not keep the hosts from seeing each other's claims every tick.
	dirty, err := s.Vault.Uncommitted(ctx)
	if err != nil {
		return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
	}
	report.Blocked = dirty

	if len(report.Blocked) == 0 {
		if report.Marked, err = s.Vault.MarkLedgers(ctx); err != nil {
			return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
		}
		if report.Pulled, err = s.Vault.Pull(ctx); err != nil {
			return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
		}
		if report.Pushed, err = s.Vault.Push(ctx); err != nil {
			return report, fmt.Errorf("syncing the vault on %s: %w", s.Host, err)
		}
	}

	// A host whose vault half never ran is not level, and the note says it was.
	// So nothing is recorded, the beads half runs anyway, and what is in the way
	// is handed back afterwards — with a status of its own, because nothing here
	// is broken.
	if len(report.Blocked) > 0 {
		retried, err := s.syncTracker(ctx)
		if err != nil {
			return report, fmt.Errorf("syncing the beads database on %s: %w", s.Host, err)
		}
		report.Retried = retried
		return report, &VaultBlocked{Host: s.Host, Files: report.Blocked}
	}

	// The vault half is level, so this is the moment the host is. The note goes
	// in before the beads cycle, because that cycle is what pushes it: written
	// after, it would wait for the next one. If the cycle then halts nothing was
	// pushed, and the note is put back the way it was.
	return s.syncBeadsRecordingLevel(ctx, report)
}

// syncBeadsRecordingLevel runs the beads half with the note of when this host
// was level already in the database, so that the one cycle carries it out.
//
// A note that cannot be written does not keep the cycle from running — hosts
// that stay level in beads keep working — but it is still a failure, said after
// the cycle, and nothing is claimed to have been recorded.
func (s Sync) syncBeadsRecordingLevel(ctx context.Context, report SyncReport) (SyncReport, error) {
	key := LastSyncKey(s.Host)
	at := s.now()
	before, noteErr := s.Tracker.Note(ctx, key)
	if noteErr == nil {
		noteErr = s.Tracker.SetNote(ctx, key, at.UTC().Format(LastSyncFormat))
	}

	// The counts go out in the same cycle. They are a report and not a claim
	// that this host was level, so a cycle that halts leaves them where they are:
	// the next one carries fresher ones. They are best effort: a note that cannot
	// be written costs the other host a reading, and must not cost this one its
	// dispatch.
	if held := ReadHostTicks(ctx, s.Ticks); held.Known() {
		_ = s.Tracker.SetNote(ctx, TicksKey(s.Host), held.Note())
	}

	retried, err := s.syncTracker(ctx)
	if err != nil {
		err = fmt.Errorf("syncing the beads database on %s: %w", s.Host, err)
		if noteErr == nil {
			if restoreErr := s.restoreNote(ctx, key, before); restoreErr != nil {
				err = fmt.Errorf("%w; and taking back the note of when %s was level: %v", err, s.Host, restoreErr)
			}
		}
		return report, err
	}
	report.Retried = retried
	if noteErr != nil {
		return report, fmt.Errorf("recording when %s was last level: %w", s.Host, noteErr)
	}
	report.At = at
	return report, nil
}

// syncTracker runs one tracker synchronisation cycle. A merge conflict (bd
// exit 2) gets exactly one retry, after a short wait: the friction that
// motivated this was a conflict that halted `mw sync` and then cleared on its
// own, by hand, forty seconds later. Nothing else is retried — 4 because no
// retry can help, 3 because the tracker has already spent its own.
//
// A retry that clears reports the notice to print, naming what bd said the
// first time, and no error. A retry that does not clear returns the second
// halt with the first attempt's words folded in, so that a person reading it
// sees both.
func (s Sync) syncTracker(ctx context.Context) (string, error) {
	err := s.Tracker.Sync(ctx)
	halt, isHalt := Halted(err)
	if !isHalt || halt.Code != 2 {
		return "", err
	}
	firstSaid := halt.Said

	if waitErr := s.wait(ctx, ConflictRetryDelay); waitErr != nil {
		return "", err
	}
	if retryErr := s.Tracker.Sync(ctx); retryErr == nil {
		return fmt.Sprintf("conflict cleared on retry (bd said: %s)", firstSaid), nil
	} else if retryHalt, ok := Halted(retryErr); ok {
		retryHalt.Said = fmt.Sprintf("%s; retried, and bd said: %s", firstSaid, retryHalt.Said)
		return "", retryHalt
	} else {
		return "", retryErr
	}
}

// wait pauses for the given duration, or ends early when ctx does.
func (s Sync) wait(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// restoreNote puts a note back the way it was: the value it had, or no value at
// all when it had none.
func (s Sync) restoreNote(ctx context.Context, key, before string) error {
	if before == "" {
		return s.Tracker.ClearNote(ctx, key)
	}
	return s.Tracker.SetNote(ctx, key, before)
}

// now is the clock this sync reads.
func (s Sync) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

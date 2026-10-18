package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultNudgeAfter is how long a claimed story may run with nothing landed,
// refused or blocked mailed to the Mayor about it before the quiet alarm
// names it, when nothing says otherwise. infrastructure/config.
// DefaultNudgeAfterMinutes is the same limit, in minutes.
const DefaultNudgeAfter = 60 * time.Minute

// DefaultNudgeSyncStale is how long another host's last recorded sync may be
// behind before the quiet alarm names it, when nothing says otherwise.
// infrastructure/config.DefaultNudgeSyncStaleMinutes is the same limit, in
// minutes.
const DefaultNudgeSyncStale = 20 * time.Minute

// MayorNeedAfter is how long a need may wait on the Mayor before the quiet
// alarm names it.
const MayorNeedAfter = 30 * time.Minute

// TranscriptLive is how recently a running session's transcript must have
// been written for the quiet alarm to count it as working, however long the
// story has gone with nothing mailed about it.
const TranscriptLive = 15 * time.Minute

// TranscriptWrites is the port the quiet alarm reads a running session's last
// sign of work through: when the harness last wrote its record of it. It is
// read-only and costs no fuel.
type TranscriptWrites interface {
	// LastWrite reports when the harness last wrote anything of the sessions
	// run in dir. It is the zero time when the harness has recorded nothing
	// there, which is not an error.
	LastWrite(ctx context.Context, dir string) (time.Time, error)
}

// ClaimTranscripts is the port the check for a session that never started reads
// the harness's record through: what was said in the worktree, and when the
// harness last wrote there.
type ClaimTranscripts interface {
	TranscriptTail
	TranscriptWrites
}

// HostSign is the newest sign of life seen of a host besides its sync note:
// when it was, and what it was, as it reads inside a clause ("vault commit").
// The zero value is no sign at all.
type HostSign struct {
	At   time.Time
	What string
}

// HostSigns is the port the quiet alarm reads a host's other signs of life
// through: what the vault holds of it, such as a commit signed mw@<host>. It is
// read-only and costs no fuel.
type HostSigns interface {
	// LastSign reports the newest sign of life of a host. It is the zero
	// HostSign when there is none, which is not an error.
	LastSign(ctx context.Context, host string) (HostSign, error)
}

// ClaimLease is how long the tracker's lease on a claim runs from the claim or
// from its last heartbeat: bd's fixed five minutes (HeartbeatInterval). A
// story's lease expiry less this is when its session was last known to be
// alive.
const ClaimLease = 5 * time.Minute

// NudgeClause is one reason the quiet alarm has to speak: a story that has run
// long with nothing mailed about it, or another host whose last sync has gone
// stale.
type NudgeClause struct {
	// Key identifies the condition, so that a caller can damp a condition still
	// holding rather than repeat it every tick: the story's id, or "host:<name>"
	// for a host.
	Key string
	// Text is the clause itself, as it reads inside the one line the mail
	// notifier types: "mw-gq6.30 in progress 73 min, no mail" or "laptop last
	// synced 31 min ago".
	Text string
}

// Nudge finds what the mail notifier's quiet alarm has to say on this host,
// right now: which of this host's claimed stories have run long with nothing
// mailed to the Mayor about them, and which other host's last recorded sync
// has gone stale. It reuses the same reading Status does — WorkInHand once,
// and the run state and other-host notes narrowed from it — and writes
// nothing: no story is claimed, no note is left, no mail is sent.
type Nudge struct {
	Tracker WorkTracker
	// Notes is where another host's last sync is read from. A nil Notes leaves
	// every other host out, the same as a nil Status.Notes does.
	Notes TrackerNotes

	// Host is which of the factory's hosts this is read for.
	Host string

	// Mayor is where the needs waiting on the Mayor are read from: one older
	// than MayorNeedAfter is a clause of its own, keyed "mayor.<bead>". A nil
	// Mayor leaves them out.
	Mayor MayorNeeds

	// Home is the vault's home file. The alarm is the home's: on a host the
	// file says is not home, Run says nothing at all. A nil Home, or a home
	// that cannot be told, leaves the alarm running as it always did.
	Home HomeFile

	// SyncHalt is this host's own mark of a halted sync, read straight rather
	// than through the tracker, the same as Status.SyncHalt: the one thing a
	// halt cannot carry is word of itself, since nothing it writes is
	// published until a later sync gets through. When it is set on a host
	// that keeps a copy of its own (remote), the other hosts' "last synced"
	// clauses would be read off a note this host cannot currently refresh, so
	// they are left out in favour of the one clause naming this host's own
	// halt; SyncMode says when they are not. A nil SyncHalt reads as never
	// halted.
	SyncHalt SyncHaltMarker

	// CloseOuts is where the close-outs running on this host are read from: a
	// story whose close-out is running is not quiet, however long it has been
	// claimed. A nil CloseOuts leaves every claimed story judged by its age.
	CloseOuts CloseOutMarks

	// SyncMode is how this host's beads are synced. On a host that reads the
	// one database every host shares (backup, shared) another host's note of
	// its last sync is read live, not off this host's own copy, so this
	// host's own halt says nothing of it: the halt is named, and the other
	// hosts' ages are kept beside it. Empty reads BeadsSyncRemote.
	SyncMode BeadsSyncMode

	// Transcripts is where the harness's record of each running session is
	// read: a story whose transcript was written within TranscriptLive is not
	// quiet, and a story named says how long its transcript has been quiet.
	// Rigs is each rig's checkout on this host, by name: the story's worktree,
	// whose sessions are read, is beside it. A nil Transcripts, or a rig not
	// in Rigs, judges the story by its mail and age alone.
	Transcripts TranscriptWrites
	Rigs        map[string]string

	// Signs is where a host's other signs of life are read, so that a Boost
	// that syncs only on its dispatch ticks is not called quiet while it is
	// landing and pushing (mw-gq6.505). A nil Signs, or signs that cannot be
	// read, judge a host by its sync and its claims alone.
	Signs HostSigns

	// NudgeAfter is how long a claimed story may run with nothing mailed about
	// it before it is named. Zero reads DefaultNudgeAfter.
	NudgeAfter time.Duration
	// SyncStale is how long another host's last recorded sync may be behind
	// before it is named. Zero reads DefaultNudgeSyncStale.
	SyncStale time.Duration

	// Now is the clock elapsed time is measured against. The zero value reads
	// the real one.
	Now func() time.Time
}

// Run reads the clauses currently true, this host's claimed stories first,
// then the other hosts, in no particular further order within each. A host the
// vault's home file says is not home has none.
func (n Nudge) Run(ctx context.Context) ([]NudgeClause, error) {
	switch {
	case n.Tracker == nil:
		return nil, fmt.Errorf("reading the quiet alarm: there is no work tracker to read it from")
	case n.Host == "":
		return nil, fmt.Errorf("reading the quiet alarm: which host is this? set MW_HOST, or host in the config file")
	}

	if n.Home != nil {
		home, err := IsHome(ctx, n.Home, n.Host)
		if _, unknown := HomeUnknownIn(err); err != nil && !unknown {
			return nil, err
		}
		if err == nil && !home {
			return nil, nil
		}
	}

	s := Status{Tracker: n.Tracker, Notes: n.Notes, Host: n.Host, Now: n.Now}
	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading what is in hand: %w", err)
	}

	var clauses []NudgeClause
	now := s.now()
	closing := closingOut(ctx, n.CloseOuts)
	for _, detail := range work.RunningOn(n.Host) {
		since := detail.QuietSince()
		if detail.Hitl() || since.IsZero() {
			continue
		}
		// A close-out that is running — landing the story, or waiting its turn
		// or for the host to calm — is the story being worked, not a quiet one.
		if _, running := closing[detail.Story.ID]; running {
			continue
		}
		// Only the run state is judged here: the formula's open steps, the
		// harness's stop and the session's turns that Status reads beside it
		// are no part of the alarm, and each is a bd for a story every minute
		// (mw-gq6.613).
		run, err := n.Tracker.StoryState(ctx, detail.Story.ID, RunState)
		if err != nil {
			return nil, fmt.Errorf("reading the run state of %s: %w", detail.Story.ID, err)
		}
		rs := RunningStory{Detail: detail, Run: run}
		// A run recorded as anything but running (or, before it is ever
		// recorded, empty) has already been mailed to the Mayor: RunBlocked is
		// set only where mw next also sends the Refused or Blocked mail.
		if rs.Run == RunBlocked {
			continue
		}
		// A story queued for batch landing has no session to be quiet: it
		// waits for its batch (plans/0117). Nor has one handed back for its
		// close-out alone, which waits for the next dispatch tick (mw-gq6.467).
		if rs.Queued() || rs.HandedBack() {
			continue
		}
		elapsed := now.Sub(since)
		if elapsed <= n.nudgeAfter() {
			continue
		}
		text := fmt.Sprintf("%s in progress %d min, no mail", detail.Story.ID, minutes(elapsed))
		// A session whose transcript is still being written is working, however
		// long it has gone with nothing mailed (mw-gq6.477): only a story quiet
		// in both is named, with how long its transcript has been quiet.
		if written, known := n.lastWrite(ctx, detail); known {
			if written.IsZero() {
				text += ", no transcript"
			} else if quiet := now.Sub(written); quiet <= TranscriptLive {
				continue
			} else {
				text += fmt.Sprintf(", transcript quiet %d min", minutes(quiet))
			}
		}
		clauses = append(clauses, NudgeClause{Key: detail.Story.ID, Text: text})
	}

	if n.Mayor != nil {
		// Needs that cannot be read are left out rather than every other
		// clause: the alarm must still name a stale host.
		needs, _ := n.Mayor.MayorNeeds(ctx, n.hitlBeads(ctx, work))
		for _, need := range needs {
			since, err := time.Parse(time.RFC3339, need.Since)
			if err != nil || need.Bead == "" {
				continue
			}
			if waited := now.Sub(since); waited > MayorNeedAfter {
				clauses = append(clauses, NudgeClause{
					Key:  "mayor." + need.Bead,
					Text: fmt.Sprintf("%s waiting on the Mayor %d min (%s)", need.Bead, minutes(waited), need.Kind),
				})
			}
		}
	}

	if n.SyncHalt != nil {
		if info, there, err := n.SyncHalt.Read(ctx); err != nil {
			return nil, fmt.Errorf("reading whether %s's own sync is halted: %w", n.Host, err)
		} else if there && n.SyncMode.OneDatabase() {
			what := "own sync"
			if n.SyncMode == BeadsSyncBackup {
				what = "beads backup"
			}
			clauses = append(clauses, NudgeClause{
				Key:  "sync:" + n.Host,
				Text: fmt.Sprintf("%s's %s halted since %s (%s)", n.Host, what, info.At.UTC().Format(LastSyncFormat), info.Said),
			})
		} else if there {
			clauses = append(clauses, NudgeClause{
				Key: "sync:" + n.Host,
				Text: fmt.Sprintf("%s's own sync halted since %s (%s); other hosts' ages are stale",
					n.Host, info.At.UTC().Format(LastSyncFormat), info.Said),
			})
			return clauses, nil
		}
	}

	others, err := s.elsewhere(ctx, work)
	if err != nil {
		return nil, err
	}
	for _, w := range others {
		if w.LastSync.IsZero() || w.Silent <= n.syncStale() || !w.holdsClaim() {
			continue
		}
		// A Boost syncs on its dispatch ticks, so its sync age alone is not a
		// sign it is quiet: it is named only when nothing else of it is newer
		// than the limit either (mw-gq6.505).
		text := fmt.Sprintf("%s last synced %d min ago", w.Host, minutes(w.Silent))
		if sign := n.newestSign(ctx, w); sign.At.After(w.LastSync) {
			quiet := now.Sub(sign.At)
			if quiet <= n.syncStale() {
				continue
			}
			text += fmt.Sprintf(", newest sign of life %s %d min ago", withArticle(sign.What), minutes(quiet))
		}
		clauses = append(clauses, NudgeClause{Key: "host:" + w.Host, Text: text})
	}

	return clauses, nil
}

// hitlBeads are the beads labelled hitl that are open and ready anywhere or
// claimed on this host: the ones a person must be present for, the same as
// Status lists. A list that cannot be read is empty.
func (n Nudge) hitlBeads(ctx context.Context, work WorkInHand) []StoryDetail {
	var hitl []StoryDetail
	for _, d := range work.RunningOn(n.Host) {
		if d.Hitl() {
			hitl = append(hitl, d)
		}
	}
	ready, err := n.Tracker.ReadyWithLabel(ctx, LabelHitl)
	if err != nil {
		return hitl
	}
	return append(hitl, ready...)
}

// lastWrite is when the harness last wrote its record of the story's sessions,
// zero when it has recorded none; known is false when that cannot be told here
// — no Transcripts, the story's rig not checked out on this host, or the record
// unreadable — and the story is judged by its mail and age alone.
func (n Nudge) lastWrite(ctx context.Context, detail StoryDetail) (written time.Time, known bool) {
	rig := n.Rigs[detail.Merged().Rig]
	if n.Transcripts == nil || rig == "" {
		return time.Time{}, false
	}
	written, err := n.Transcripts.LastWrite(ctx, WorktreeDir(rig, detail.Story.ID))
	if err != nil {
		return time.Time{}, false
	}
	return written, true
}

// newestSign is the newest sign of life of a host besides its sync note
// (NewestSign).
func (n Nudge) newestSign(ctx context.Context, w HostWork) HostSign {
	return NewestSign(ctx, n.Signs, w.Host, w.Stories)
}

// SignHeartbeat is what a renewed lease on a story a host claimed is called in
// a line that names the host's newest sign of life.
const SignHeartbeat = "story heartbeat"

// NewestSign is the newest sign of life of a host besides its sync note: the
// claims its stories were last made or sent back under, the last renewal of
// the lease on any story it holds (a Boost whose sessions all run long makes
// no vault commit and no claim for a while, but its heartbeats go on,
// mw-gq6.523), and what the vault holds of it. stories are the ones pathed to
// the host. Signs that cannot be read are left out, and the host is judged by
// the rest. It is the one reading mw status, the Postern view, the doctor and
// the quiet alarm all judge a host quiet by.
func NewestSign(ctx context.Context, signs HostSigns, host string, stories []StoryDetail) HostSign {
	var newest HostSign
	for _, d := range stories {
		if d.ClaimedAt.After(newest.At) {
			newest = HostSign{At: d.ClaimedAt, What: "claim"}
		}
		if d.SentBackAt.After(newest.At) {
			newest = HostSign{At: d.SentBackAt, What: "send-back"}
		}
		if strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) && !d.LeaseExpires.IsZero() {
			if beat := d.LeaseExpires.Add(-ClaimLease); beat.After(newest.At) {
				newest = HostSign{At: beat, What: SignHeartbeat}
			}
		}
	}
	if signs != nil {
		if sign, err := signs.LastSign(ctx, host); err == nil && sign.At.After(newest.At) {
			newest = sign
		}
	}
	return newest
}

// withArticle puts "a" or "an" before a noun phrase.
func withArticle(what string) string {
	if what != "" && strings.ContainsRune("aeiou", rune(what[0])) {
		return "an " + what
	}
	return "a " + what
}

// holdsClaim reports whether any of the stories pathed to this host is claimed:
// a host that is simply off, with nothing of the home's in its hands, is not an
// alarm however long it has been silent.
func (w HostWork) holdsClaim() bool {
	for _, d := range w.Stories {
		if strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) {
			return true
		}
	}
	return false
}

// nudgeAfter is how long a claimed story may run with nothing mailed about it.
func (n Nudge) nudgeAfter() time.Duration {
	if n.NudgeAfter <= 0 {
		return DefaultNudgeAfter
	}
	return n.NudgeAfter
}

// syncStale is how long another host's last recorded sync may be behind.
func (n Nudge) syncStale() time.Duration {
	if n.SyncStale <= 0 {
		return DefaultNudgeSyncStale
	}
	return n.SyncStale
}

// minutes is a duration in whole minutes, rounded to the nearest.
func minutes(d time.Duration) int { return int(d.Round(time.Minute) / time.Minute) }

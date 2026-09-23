package application

import (
	"context"
	"fmt"
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

	// SyncHalt is this host's own mark of a halted sync, read straight rather
	// than through the tracker, the same as Status.SyncHalt: the one thing a
	// halt cannot carry is word of itself, since nothing it writes is
	// published until a later sync gets through. When it is set, the other
	// hosts' "last synced" clauses would be read off a note this host cannot
	// currently refresh, so they are left out in favour of the one clause
	// naming this host's own halt. A nil SyncHalt reads as never halted.
	SyncHalt SyncHaltMarker

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
// then the other hosts, in no particular further order within each.
func (n Nudge) Run(ctx context.Context) ([]NudgeClause, error) {
	switch {
	case n.Tracker == nil:
		return nil, fmt.Errorf("reading the quiet alarm: there is no work tracker to read it from")
	case n.Host == "":
		return nil, fmt.Errorf("reading the quiet alarm: which host is this? set MW_HOST, or host in the config file")
	}

	s := Status{Tracker: n.Tracker, Notes: n.Notes, Host: n.Host, Now: n.Now}
	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading what is in hand: %w", err)
	}

	var clauses []NudgeClause
	now := s.now()
	for _, detail := range work.RunningOn(n.Host) {
		if detail.Hitl() || detail.Started.IsZero() {
			continue
		}
		rs, err := s.running(ctx, detail)
		if err != nil {
			return nil, err
		}
		// A run recorded as anything but running (or, before it is ever
		// recorded, empty) has already been mailed to the Mayor: RunBlocked is
		// set only where mw next also sends the Refused or Blocked mail.
		if rs.Run == RunBlocked {
			continue
		}
		if elapsed := now.Sub(detail.Started); elapsed > n.nudgeAfter() {
			clauses = append(clauses, NudgeClause{
				Key:  detail.Story.ID,
				Text: fmt.Sprintf("%s in progress %d min, no mail", detail.Story.ID, minutes(elapsed)),
			})
		}
	}

	if n.SyncHalt != nil {
		if info, there, err := n.SyncHalt.Read(ctx); err != nil {
			return nil, fmt.Errorf("reading whether %s's own sync is halted: %w", n.Host, err)
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
		if w.LastSync.IsZero() || w.Silent <= n.syncStale() {
			continue
		}
		clauses = append(clauses, NudgeClause{
			Key:  "host:" + w.Host,
			Text: fmt.Sprintf("%s last synced %d min ago", w.Host, minutes(w.Silent)),
		})
	}

	return clauses, nil
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

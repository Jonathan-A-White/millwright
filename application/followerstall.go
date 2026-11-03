package application

import (
	"context"
	"fmt"
	"time"
)

// FollowerStallLimit is how far the follower may fall behind the beads before
// mw doctor says it has stalled: five minutes, for the follower writes a bead's
// event within seconds, and the Governor's phone shows a bead's move within ten
// (mw-gq6.893: on 2026-11-02 it stood still 20 minutes and nothing said so).
const FollowerStallLimit = 5 * time.Minute

// FollowerStallCheck is the mw doctor check that finds mw events follow alive
// and doing nothing (mw-gq6.893). With the follower up it is not ok when
//
//   - the newest change the beads hold is more than Limit past the newest
//     bead or mail event the follower wrote (EventNewest.Other): the beads
//     moved and the log did not, so no nudge, no spring and no event went out; or
//   - the view was last written more than Limit before the beads' newest
//     change and more than Limit ago: the Governor's phone shows a state the
//     beads left. A change that leaves the view as it was is not written again
//     (PosternViewMemo), so this one can fire on a long run of such changes.
//
// It changes nothing, and says cannot-tell, which mw doctor writes as a note
// the Millhand and the Mayor read, once for each reason; the reason names the
// minutes only in whole tens, so that the same stall read again reads the same.
type FollowerStallCheck struct {
	// Up says whether the follower runs on this host. A host without it has
	// nothing to stall.
	Up func(ctx context.Context) (bool, error)
	// Log is the event log the follower writes.
	Log EventLog
	// Feed is the beads' changes.
	Feed BeadFeed
	// ViewWritten is when the view the backend serves was last written; false
	// when there is none.
	ViewWritten func(ctx context.Context) (time.Time, bool, error)
	// Limit is how far behind is a stall; zero is FollowerStallLimit.
	Limit time.Duration
	// Now is the clock; nil reads time.Now.
	Now func() time.Time
}

var _ DoctorCheck = (*FollowerStallCheck)(nil)

// Name implements DoctorCheck.
func (c *FollowerStallCheck) Name() string { return "follower-stalled" }

func (c *FollowerStallCheck) limit() time.Duration {
	if c.Limit > 0 {
		return c.Limit
	}
	return FollowerStallLimit
}

// Probe implements DoctorCheck.
func (c *FollowerStallCheck) Probe(ctx context.Context) (Verdict, string) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	if c.Up != nil {
		up, err := c.Up(ctx)
		if err != nil {
			return DoctorCannotTell, fmt.Sprintf("whether the follower runs could not be read: %v", err)
		}
		if !up {
			return DoctorOK, "n/a: the follower does not run on this host"
		}
	}
	newest, err := NewestOf(ctx, c.Log)
	if err != nil {
		return DoctorCannotTell, fmt.Sprintf("the event log's newest events could not be read: %v", err)
	}
	var viewAt time.Time
	viewThere := false
	if c.ViewWritten != nil {
		if viewAt, viewThere, err = c.ViewWritten(ctx); err != nil {
			return DoctorCannotTell, fmt.Sprintf("when the view was written could not be read: %v", err)
		}
	}
	// The beads are asked for the changes from the older of the two marks, and
	// never from further back than a day: a follower that has written nothing
	// for longer is as stalled as one that stopped a day ago.
	since := newest.Other.Ts
	if viewThere && viewAt.Before(since) {
		since = viewAt
	}
	if floor := now.Add(-24 * time.Hour); since.Before(floor) {
		since = floor
	}
	changes, err := c.Feed.BeadChanges(ctx, since)
	if err != nil {
		return DoctorCannotTell, fmt.Sprintf("the beads' changes could not be read: %v", err)
	}
	var latest time.Time
	for _, ch := range changes {
		if ch.At.After(latest) && !ch.At.After(now.Add(time.Minute)) {
			latest = ch.At
		}
	}
	if latest.IsZero() {
		return DoctorOK, ""
	}
	limit := c.limit()
	var found []string
	if behind := latest.Sub(newest.Other.Ts); behind > limit {
		found = append(found, fmt.Sprintf("the newest bead change is %s past the newest bead or mail event written (event %d at %s), so no event, nudge or spring has gone out for it",
			wholeTens(behind), newest.Other.Seq, newest.Other.Ts.UTC().Format(LastSyncFormat)))
	}
	if viewThere && latest.Sub(viewAt) > limit && now.Sub(viewAt) > limit {
		found = append(found, fmt.Sprintf("the view was last written %s before the newest bead change (at %s)",
			wholeTens(latest.Sub(viewAt)), viewAt.UTC().Format(LastSyncFormat)))
	}
	if len(found) == 0 {
		return DoctorOK, ""
	}
	return DoctorCannotTell, "STALLED: the follower is up and " + joinWithAnd(found) +
		". Look: journalctl --user -u mw-view-follow.service (a send to the postern backend that hangs shows there); restart it: systemctl --user restart mw-view-follow.service"
}

// wholeTens is d to the minute, held to a whole ten of them once it is past
// ten, so a stall that grows by a minute between two runs reads as it did.
func wholeTens(d time.Duration) string {
	m := int(d / time.Minute)
	if m >= 10 {
		m -= m % 10
		return fmt.Sprintf("over %d minutes", m)
	}
	return fmt.Sprintf("%d minutes", m)
}

func joinWithAnd(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", and " + p
	}
	return out
}

// Cure implements DoctorCheck. Nothing is cured: Probe never says faulty.
func (c *FollowerStallCheck) Cure(context.Context) error { return nil }

// Damper implements DoctorCheck.
func (c *FollowerStallCheck) Damper() (time.Duration, int) { return time.Hour, 1 }

// WayBack implements DoctorCheck.
func (c *FollowerStallCheck) WayBack() string { return "nothing was changed" }

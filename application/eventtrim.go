package application

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// DefaultEventKeep is how many of the newest events a trim leaves in the log
// whatever the cursors say.
const DefaultEventKeep = 5000

// EventArchive is where the log's old events go: dated files beside the log,
// read back when a tail reaches before what the log keeps.
type EventArchive interface {
	// Trim moves every event of the log with a seq up to upTo into the
	// archive file of day (2006-01-02), and reports how many. The seqs of
	// the events left are unchanged, and so is the head.
	Trim(ctx context.Context, upTo uint64, day string) (moved int, err error)
	// First is the seq of the oldest event the log still holds, 0 for an
	// empty log.
	First(ctx context.Context) (uint64, error)
	// Archived is every archived event after seq, oldest first.
	Archived(ctx context.Context, seq uint64) ([]events.Event, error)
}

// EventTrimmer is what EventFollow calls, at most once a UTC day, to keep the
// log small: EventTrim.
type EventTrimmer interface {
	Trim(ctx context.Context) error
}

// EventTrim moves the log's old events into the archive, so the log stays
// small (mw-gq6.291). An event is old when every reader is past it: its seq
// is at or below the lowest of the seat nudge cursors and the control cursor
// (NudgeCursors), at or below what the shipper has sent, and below the first
// batch still waiting for the chain (which is read again from the log); and
// it is not one of the newest Keep. The follower's own cursor is the beads'
// time, not a seq, and holds nothing back.
type EventTrim struct {
	Log     EventLog
	Archive EventArchive
	Nudges  NudgeCursors
	Ship    ShipStates
	// Keep is how many of the newest events stay in the log; zero means
	// DefaultEventKeep.
	Keep uint64
	// Now is the clock that dates the archive file; nil is time.Now.
	Now func() time.Time
	// Out, when set, is told what was moved.
	Out io.Writer
}

// TrimResult is what a trim did.
type TrimResult struct {
	// Moved is how many events went to the archive; UpTo is the last seq
	// the cut allowed, which is where the log now starts after.
	Moved int
	UpTo  uint64
}

// Trim implements EventTrimmer.
func (t EventTrim) Trim(ctx context.Context) error {
	_, err := t.Run(ctx)
	return err
}

// Run cuts the log as far as the readers allow.
func (t EventTrim) Run(ctx context.Context) (TrimResult, error) {
	if t.Log == nil || t.Archive == nil || t.Nudges == nil || t.Ship == nil {
		return TrimResult{}, fmt.Errorf("trimming the event log needs the log, an archive, the seats' cursors and the shipper's state")
	}
	keep := t.Keep
	if keep == 0 {
		keep = DefaultEventKeep
	}
	head, err := t.Log.Head(ctx)
	if err != nil {
		return TrimResult{}, err
	}
	if head <= keep {
		return TrimResult{}, t.say(TrimResult{})
	}
	upTo := head - keep
	cursors, err := t.Nudges.Load(ctx)
	if err != nil {
		return TrimResult{}, err
	}
	for _, seq := range cursors {
		upTo = min(upTo, seq)
	}
	state, err := t.Ship.Load(ctx)
	if err != nil {
		return TrimResult{}, err
	}
	upTo = min(upTo, state.Shipped)
	for _, r := range state.Pending {
		if r.From > 0 {
			upTo = min(upTo, r.From-1)
		}
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	moved, err := t.Archive.Trim(ctx, upTo, now().UTC().Format("2006-01-02"))
	if err != nil {
		return TrimResult{}, fmt.Errorf("moving old events to the archive: %w", err)
	}
	return TrimResult{Moved: moved, UpTo: upTo}, t.say(TrimResult{Moved: moved, UpTo: upTo})
}

func (t EventTrim) say(r TrimResult) error {
	if t.Out == nil {
		return nil
	}
	if r.Moved == 0 {
		_, err := fmt.Fprintln(t.Out, "Nothing to move: every event is one a reader still needs, or among the newest kept.")
		return err
	}
	_, err := fmt.Fprintf(t.Out, "Moved %d events, up to seq %d, to the archive.\n", r.Moved, r.UpTo)
	return err
}

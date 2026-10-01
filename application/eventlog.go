package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// EventLog is the home's sequenced, append-only log of events: every
// transition of the factory's machines, numbered by the home (mw-6ww.55, Q4
// A). Its seq is the cursor a handover or the Governor's app reads from.
type EventLog interface {
	// Append numbers events on from the head, in order, and writes them all
	// or none: an event that is not one the factory writes (events.Event's
	// Validate) refuses the whole batch. It reports the last seq given.
	Append(ctx context.Context, evs []events.Event) (uint64, error)
	// Since is every event after seq, oldest first; Since(0) is the whole log.
	Since(ctx context.Context, seq uint64) ([]events.Event, error)
	// Head is the last seq given, 0 for an empty log.
	Head(ctx context.Context) (uint64, error)
}

// EventEmit appends one event of a writer's own: a scheduled job's
// transition, which no bead records (mw-6ww.55, Q7 rule 1). It is stamped
// now, in the normal lane, and refused before anything is written when its
// kind's machine forbids it. With Emergency it is in the emergency lane,
// which the follower's shipper sends alone and at once (docs/events.md, "The
// batch") instead of in the next 2 s batch.
type EventEmit struct {
	Log       EventLog
	Now       func() time.Time
	Event     events.Event
	Emergency bool
}

// Run appends the event and returns it as written, seq and all.
func (e EventEmit) Run(ctx context.Context) (events.Event, error) {
	ev := e.Event
	ev.Ts = e.Now().UTC()
	ev.Lane = events.LaneNormal
	if e.Emergency {
		ev.Lane = events.LaneEmergency
	}
	// Seq 1 stands in for the seq Append gives, so Validate can be asked
	// before anything is written.
	check := ev
	check.Seq = 1
	if err := check.Validate(); err != nil {
		return events.Event{}, err
	}
	last, err := e.Log.Append(ctx, []events.Event{ev})
	if err != nil {
		return events.Event{}, err
	}
	ev.Seq = last
	return ev, nil
}

// DefaultEventTailEvery is how often EventTail with Follow reads the log
// again.
const DefaultEventTailEvery = 500 * time.Millisecond

// EventTail prints the events after Since, one EventLine each, and with
// Follow goes on printing what is appended until ctx ends.
type EventTail struct {
	Log    EventLog
	Since  uint64
	Follow bool
	// Every is the wait between two reads with Follow; zero means
	// DefaultEventTailEvery.
	Every time.Duration
	// Sleep waits d or until ctx ends; a test replaces it. Nil waits on a
	// timer.
	Sleep func(ctx context.Context, d time.Duration) error
	Out   io.Writer
}

// Run prints and, with Follow, keeps printing; a context that ends is a clean
// stop.
func (t EventTail) Run(ctx context.Context) error {
	every := t.Every
	if every <= 0 {
		every = DefaultEventTailEvery
	}
	sleep := t.Sleep
	if sleep == nil {
		sleep = sleepFor
	}
	since := t.Since
	for {
		evs, err := t.Log.Since(ctx, since)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, e := range evs {
			fmt.Fprintln(t.Out, EventLine(e))
			since = e.Seq
		}
		if !t.Follow || sleep(ctx, every) != nil {
			return nil
		}
	}
}

// EventLine is one event on one line: seq, time, kind, bead, actor, the
// transition, then the detail. An empty bead or actor, or a kind with no
// machine, shows "-"; the empty from is "(start)".
func EventLine(e events.Event) string {
	dash := func(s string) string {
		if s == "" {
			return "-"
		}
		return s
	}
	move := "-"
	if e.To != "" || e.From != "" {
		from := e.From
		if from == events.Start {
			from = "(start)"
		}
		move = from + "->" + e.To
	}
	line := fmt.Sprintf("%d %s %s %s %s %s", e.Seq, e.Ts.UTC().Format(time.RFC3339), e.Kind, dash(e.Bead), dash(e.Actor), move)
	if detail := strings.Join(strings.Fields(e.Detail), " "); detail != "" {
		line += " " + detail
	}
	return line
}

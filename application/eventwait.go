package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// DefaultEventWaitLimit is how long mw events wait waits when nothing says
// otherwise.
const DefaultEventWaitLimit = 50 * time.Minute

// DefaultEventWaitEvery is how often EventWait looks at the log's head.
const DefaultEventWaitEvery = time.Second

// EventWait blocks until the home's log holds an event the seat subscribed
// to, then prints the matching events and returns, so that a seat's harness,
// running it in the background, wakes on its exit. While it waits it reads
// the log's head once a second and calls no bd: it costs no tokens.
//
// It ends on its first match with a line saying how many events and how to
// read them (the same line the follower types into an idle seat's pane),
// then one EventLine per match; or, on Limit, with a line saying nothing came.
//
// A handover of the seat ends it too, whatever the kinds (mw-jrx0s.11). The
// old session's wait is told "handed over at N" after the events it
// subscribed to up to N, and none past it; the successor's wait, begun
// before the handover, is told it holds the seat from N, with the events
// past N; a successor's wait begun at or after N does not see it. That holds
// for a wait given --since; a wait begun at the head with no --since takes the
// seat from any handover appended after it began, whatever its N.
type EventWait struct {
	Log EventLog
	// Subscription is the seat and the kinds that end the wait.
	Subscription Subscription
	// Since, when set, is the seq after which events count, so events already
	// in the log end the wait at once; unset, only events after the log's
	// head when the wait began do.
	Since *uint64
	// Self is the name of the window the wait runs in, empty when it is not
	// known. It tells a handover of the seat to this window (the wait of the
	// successor, woken to take the seat) from a handover to another (the old
	// session's wait, which ends: handed over at N); a wait that does not know
	// its window is the old session's.
	Self string
	// Limit is how long to wait; zero is DefaultEventWaitLimit.
	Limit time.Duration
	// Every is the wait between two looks at the head; zero is
	// DefaultEventWaitEvery.
	Every time.Duration
	// Sleep waits d or until ctx ends; a test replaces it. Nil waits on a
	// timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	Out io.Writer
}

// Run waits and prints; it returns the matching events, none when the limit
// ended it.
func (w EventWait) Run(ctx context.Context) ([]events.Event, error) {
	if w.Log == nil || w.Subscription.Seat == "" || len(w.Subscription.Kinds) == 0 {
		return nil, fmt.Errorf("waiting for events needs a log, a seat and the kinds it waits for")
	}
	limit, every, sleep, now := w.Limit, w.Every, w.Sleep, w.Now
	if limit <= 0 {
		limit = DefaultEventWaitLimit
	}
	if every <= 0 {
		every = DefaultEventWaitEvery
	}
	if sleep == nil {
		sleep = sleepFor
	}
	if now == nil {
		now = time.Now
	}
	started := now()
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	var cursor uint64
	if w.Since != nil {
		cursor = *w.Since
	} else {
		head, err := w.Log.Head(ctx)
		if err != nil {
			return nil, err
		}
		cursor = head
	}
	start := cursor
	// A wait begun at the head (no --since) reads only events appended after it
	// began, so a handover it reads is its own to take even when it marked the
	// head (At == start, a quiet log: mw-gq6.210). A wait given --since N
	// ignores a handover that marked N or earlier.
	atHead := w.Since == nil
	watch := HandoverWatch{Seat: w.Subscription.Seat, Self: w.Self}
	for {
		head, err := w.Log.Head(ctx)
		if err == nil && head > cursor {
			var evs []events.Event
			if evs, err = w.Log.Since(ctx, cursor); err == nil {
				var matched []events.Event
				var taking *events.Event
				var took events.Handover
				for _, ev := range evs {
					cursor = ev.Seq
					if h, ok := events.HandoverOf(ev); ok && h.Seat == watch.Seat {
						if _, _, ends := watch.Ends([]events.Event{ev}); ends {
							return w.handedOver(ev, h, matched), nil
						}
						if (atHead || h.At > start) && taking == nil {
							ev := ev
							taking, took = &ev, h
						}
						continue
					}
					if w.Subscription.Matches(ev) {
						matched = append(matched, ev)
					}
				}
				if taking != nil {
					return w.takeSeat(*taking, took, matched), nil
				}
				if len(matched) > 0 {
					fmt.Fprintln(w.Out, EventsNudgeLine(w.Subscription.Seat, len(matched), matched[0].Seq-1))
					for _, ev := range matched {
						fmt.Fprintln(w.Out, EventLine(ev))
					}
					return matched, nil
				}
			}
		}
		if err != nil && ctx.Err() == nil {
			return nil, err
		}
		if sleep(ctx, every) != nil {
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, ctx.Err()
			}
			fmt.Fprintf(w.Out, "No events for %s by %s; the wait ended on its time limit (%s). Arm it again.\n",
				w.Subscription.Seat, now().UTC().Format(time.RFC3339), now().Sub(started).Round(time.Second))
			return nil, nil
		}
	}
}

// handedOver ends the old session's wait: the events it subscribed to up to
// h.At, then the line saying the seat is handed over.
func (w EventWait) handedOver(handover events.Event, h events.Handover, matched []events.Event) []events.Event {
	var mine []events.Event
	for _, ev := range matched {
		if ev.Seq <= h.At {
			mine = append(mine, ev)
		}
	}
	if len(mine) > 0 {
		fmt.Fprintln(w.Out, EventsNudgeLine(w.Subscription.Seat, len(mine), mine[0].Seq-1))
		for _, ev := range mine {
			fmt.Fprintln(w.Out, EventLine(ev))
		}
	}
	fmt.Fprintln(w.Out, HandedOverLine(h))
	return append(mine, handover)
}

// takeSeat ends the successor's wait: the line saying it holds the seat from
// h.At, then the events it subscribed to past h.At that the wait has read.
func (w EventWait) takeSeat(handover events.Event, h events.Handover, matched []events.Event) []events.Event {
	fmt.Fprintln(w.Out, HoldLine(h))
	mine := []events.Event{handover}
	for _, ev := range matched {
		if ev.Seq > h.At {
			mine = append(mine, ev)
			fmt.Fprintln(w.Out, EventLine(ev))
		}
	}
	return mine
}

// EventsNudgeLine is the line that tells a seat n events wait for it, which
// follow seq: what the wait prints first, and what the follower types.
func EventsNudgeLine(seat string, n int, since uint64) string {
	return fmt.Sprintf("New events for %s: %d. Run mw events tail --since %d.", seat, n, since)
}

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
type EventWait struct {
	Log EventLog
	// Subscription is the seat and the kinds that end the wait.
	Subscription Subscription
	// Since, when set, is the seq after which events count, so events already
	// in the log end the wait at once; unset, only events after the log's
	// head when the wait began do.
	Since *uint64
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
	for {
		head, err := w.Log.Head(ctx)
		if err == nil && head > cursor {
			var evs []events.Event
			if evs, err = w.Log.Since(ctx, cursor); err == nil {
				var matched []events.Event
				for _, ev := range evs {
					if w.Subscription.Matches(ev) {
						matched = append(matched, ev)
					}
					cursor = ev.Seq
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

// EventsNudgeLine is the line that tells a seat n events wait for it, which
// follow seq: what the wait prints first, and what the follower types.
func EventsNudgeLine(seat string, n int, since uint64) string {
	return fmt.Sprintf("New events for %s: %d. Run mw events tail --since %d.", seat, n, since)
}

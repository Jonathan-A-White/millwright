package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
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
// seat from any handover appended after it began, whatever its N. Such a wait
// with the handover among its kinds also looks back first: when the seat's
// newest handover is to its own window (Self), it holds the seat at once,
// the handover having landed before the wait was armed (mw-gq6.668), unless
// Acting is set and the seat's acting file does not name Self at that
// handover's event: a stale handover (mw-gq6.767).
type EventWait struct {
	Log EventLog
	// Subscription is the seat and the kinds that end the wait.
	Subscription Subscription
	// Since, when set, is the seq after which events count, so events already
	// in the log end the wait at once; unset, only events after the log's
	// head when the wait began do, besides the mail Box holds unread.
	Since *uint64
	// Box, when set, is the seat's mailbox. A wait with mail among its kinds and
	// no Since reads it once before the loop: mail already unread in the seat's
	// box ends the wait at once, since it is behind the head the wait would
	// start at (mw-gq6.512). The loop calls it never. Nil, or a box that cannot
	// be read, leaves the wait as it was.
	Box Mailbox
	// Acting, when set, is the text of the seat's acting file ("" for none),
	// which the look-back before arming consults: the seat is this window's only
	// when the file names it and stands at the handover's event or earlier, so a
	// handover to this window that the seat has since left (or never took, the
	// old session still acting) is stale and the wait waits (mw-gq6.767). Nil
	// leaves the look-back to the log alone.
	Acting *string
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

	if w.Since == nil && w.Box != nil && w.waitsForMail() {
		if unread, err := w.Box.Inbox(ctx, w.Subscription.Seat); err == nil && len(unread) > 0 {
			fmt.Fprintln(w.Out, UnreadMailLine(w.Subscription.Seat, len(unread)))
			return nil, nil
		}
	}

	if w.Since == nil {
		if taken, ok, err := w.heldBeforeArming(ctx); err != nil {
			return nil, err
		} else if ok {
			return taken, nil
		}
	}

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

// heldBeforeArming is the look-back of a wait that listens for a handover and
// has no Since: when the seat's newest handover in the log is to this window,
// the seat is already its own (the handover landed while the successor read its
// boot, before it armed this wait: mw-gq6.668), so it prints the hold line and
// returns that handover at once. A handover to another window, one followed by
// a handover away, a wait that does not know its window or one that does not
// listen for a handover wait as they do without it.
func (w EventWait) heldBeforeArming(ctx context.Context) ([]events.Event, bool, error) {
	if w.Self == "" || !w.waitsFor(events.KindHandover) {
		return nil, false, nil
	}
	evs, err := w.Log.Since(ctx, 0)
	if err != nil {
		return nil, false, err
	}
	var newest events.Event
	var held events.Handover
	for _, ev := range evs {
		if h, ok := events.HandoverOf(ev); ok && h.Seat == w.Subscription.Seat {
			newest, held = ev, h
		}
	}
	if newest.Seq == 0 || held.Successor != w.Self {
		return nil, false, nil
	}
	if w.Acting != nil && !actingHolds(*w.Acting, w.Self, held.At) {
		return nil, false, nil
	}
	return w.takeSeat(newest, held, nil), true, nil
}

var actingEventRe = regexp.MustCompile(`handed over at event (\d+)`)

// actingHolds reports whether an acting file's text puts the seat in the window
// self at a handover marking at: it names the window as a whole word, and when
// it says "handed over at event N", N is not past at.
func actingHolds(text, self string, at uint64) bool {
	named := regexp.MustCompile(`(^|[\s'"(])` + regexp.QuoteMeta(self) + `([\s'",)]|$)`)
	if !named.MatchString(text) {
		return false
	}
	if m := actingEventRe.FindStringSubmatch(text); m != nil {
		if n, err := strconv.ParseUint(m[1], 10, 64); err == nil && n > at {
			return false
		}
	}
	return true
}

// waitsFor reports whether kind is among the kinds the wait ends on.
func (w EventWait) waitsFor(kind string) bool {
	for _, k := range w.Subscription.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// waitsForMail reports whether mail is among the kinds the wait ends on.
func (w EventWait) waitsForMail() bool { return w.waitsFor(events.KindMail) }

// UnreadMailLine is what a wait prints when it finds mail already unread in
// the seat's box as it begins.
func UnreadMailLine(seat string, n int) string {
	return fmt.Sprintf("New events for %s: unread mail in its box (%d). Run bd mail inbox.", seat, n)
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

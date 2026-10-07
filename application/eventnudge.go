package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// MailNudgeFormat is the second line typed into a seat's pane when mail is
// among the events: the words the mail notifier types for the Mayor's box.
const MailNudgeFormat = "New mail for %s: %d message(s). Run bd mail inbox."

// NudgeCursors keeps, for each seat, the last seq the follower has told it
// of, or has seen nothing to tell it in.
type NudgeCursors interface {
	Load(ctx context.Context) (map[string]uint64, error)
	Save(ctx context.Context, cursors map[string]uint64) error
}

// EventNudger is what EventFollow calls at the end of each pass to tell the
// seats of their events: EventNudge.
type EventNudger interface {
	Nudge(ctx context.Context) error
}

// EventNudge is how a seat hears an event it subscribed to without polling
// for it (mw-6ww.55, Q7 rule 2). Each Nudge reads every seat's subscribe file
// and, for each seat with events since the last it was told of that match:
//
//   - a window that is up and idle at an empty input line is typed
//     EventsNudgeLine, then, when mail is among the events, MailNudgeFormat,
//     the second only if the pane is idle still.
//     After a line is typed the input line is read once Settle has passed: if
//     the line is still on it, Enter is pressed once more, and the log says so
//     (and says if the line is on it still). The pane is then not typed into
//     again until the line is empty. A line a busy pane queued is delivered.
//   - a window that is up and not idle is left alone: the events are told
//     again on the next Nudge;
//   - a window that is down is brought up with Spring when the seat is marked
//     spring, its reason the same line; a seat not marked is not told, and
//     reads the log from its handoff when it next comes up.
//
// A seat with no cursor yet starts at the log's head: history is not told.
// A seat's file that cannot be read is said on Err and the seat left out; a
// failure to type or spring is said and retried on the next Nudge.
type EventNudge struct {
	Log      EventLog
	Subs     SubscribeFiles
	Cursors  NudgeCursors
	Terminal ReapTerminal
	// Seats is read for a seat's acting file, to tell which of several
	// windows of the seat is the live one.
	Seats SeatFiles
	// Spring brings the seat up, for the reason given. Nil springs none.
	Spring func(ctx context.Context, seat, reason string) error
	Host   string
	// Err is where failures are said, each one once while it repeats, and
	// where a nudge that needed Enter pressed again is said.
	Err io.Writer
	// Settle is how long after typing a nudge the input line is read, to see
	// that Enter took it. Zero reads it at once.
	Settle time.Duration

	mu   sync.Mutex
	said map[string]string
}

// Nudge tells each subscribed seat of what is new for it.
func (n *EventNudge) Nudge(ctx context.Context) error {
	if n.Log == nil || n.Subs == nil || n.Cursors == nil || n.Terminal == nil {
		return fmt.Errorf("telling the seats of events needs a log, their subscriptions, a cursor and a terminal")
	}
	seats, err := n.Subs.SubscribedSeats(ctx)
	if err != nil {
		return fmt.Errorf("listing the seats' subscriptions: %w", err)
	}
	head, err := n.Log.Head(ctx)
	if err != nil {
		return err
	}
	cursors, err := n.Cursors.Load(ctx)
	if err != nil {
		return err
	}
	if cursors == nil {
		cursors = map[string]uint64{}
	}
	dirty := false
	set := func(seat string, seq uint64) {
		if have, ok := cursors[seat]; !ok || have != seq {
			cursors[seat] = seq
			dirty = true
		}
	}
	var windows []ReapWindow
	var haveWindows bool
	for _, seat := range seats {
		sub, ok, err := ReadSubscription(ctx, n.Subs, seat)
		if err != nil {
			n.say(seat, err)
			continue
		}
		n.quiet(seat)
		cur, known := cursors[seat]
		if !ok {
			continue
		}
		if !known {
			set(seat, head)
			continue
		}
		if head <= cur {
			continue
		}
		evs, err := n.Log.Since(ctx, cur)
		if err != nil {
			return err
		}
		var matched []events.Event
		mail := 0
		last := cur
		for _, ev := range evs {
			last = ev.Seq
			if sub.Matches(ev) {
				matched = append(matched, ev)
				if ev.Kind == events.KindMail {
					mail++
				}
			}
		}
		if len(matched) == 0 {
			set(seat, last)
			continue
		}
		if !haveWindows {
			if windows, err = n.Terminal.OpenWindows(ctx); err != nil {
				return fmt.Errorf("listing the windows: %w", err)
			}
			haveWindows = true
		}
		line := EventsNudgeLine(seat, len(matched), matched[0].Seq-1)
		window, found := n.windowOf(ctx, windows, seat)
		switch {
		case found:
			told, err := n.tell(ctx, window, seat, line, mail)
			if err != nil {
				n.say("tell:"+seat, err)
			} else {
				n.quiet("tell:" + seat)
			}
			if told {
				set(seat, last)
			}
		case sub.Spring && n.Spring != nil:
			if err := n.Spring(ctx, seat, line); err != nil {
				n.say("spring:"+seat, fmt.Errorf("bringing up %s: %w", seat, err))
				continue
			}
			n.quiet("spring:" + seat)
			set(seat, last)
		default:
			set(seat, last)
		}
	}
	if dirty {
		return n.Cursors.Save(ctx, cursors)
	}
	return nil
}

// tell types the nudge into the window when its pane is idle, and reports
// whether it did. A pane at work, or with text on its input line, is left
// alone and is not a failure.
func (n *EventNudge) tell(ctx context.Context, window ReapWindow, seat, line string, mail int) (bool, error) {
	state, err := n.Terminal.PaneState(ctx, window.ID)
	if err != nil {
		return false, fmt.Errorf("reading the pane of %s: %w", window.Name, err)
	}
	if state != PaneIdle {
		return false, nil
	}
	if err := n.typeAndConfirm(ctx, window, line); err != nil {
		return false, fmt.Errorf("typing the nudge into %s: %w", window.Name, err)
	}
	if mail == 0 {
		return true, nil
	}
	// Typing the first line may have woken the seat: the second goes in only
	// if the pane is idle again.
	if state, err := n.Terminal.PaneState(ctx, window.ID); err != nil || state != PaneIdle {
		return true, nil
	}
	if err := n.typeAndConfirm(ctx, window, fmt.Sprintf(MailNudgeFormat, seat, mail)); err != nil {
		return true, fmt.Errorf("typing the mail nudge into %s: %w", window.Name, err)
	}
	return true, nil
}

// typeAndConfirm types the line with TypeConfirmed and says on Err what came of
// it, once, when Enter had to be pressed again.
func (n *EventNudge) typeAndConfirm(ctx context.Context, window ReapWindow, line string) error {
	outcome, err := TypeConfirmed(ctx, n.Terminal, window, line, n.Settle)
	if err != nil {
		return err
	}
	if said := outcome.Said(window.Name, line); said != "" {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.Err != nil {
			fmt.Fprintf(n.Err, "mw events follow: %s\n", said)
		}
	}
	return nil
}

// windowOf is the seat's window: the only open one named <seat>-*, or, of
// several, the one its acting file names.
func (n *EventNudge) windowOf(ctx context.Context, windows []ReapWindow, seat string) (ReapWindow, bool) {
	var mine []ReapWindow
	for _, w := range windows {
		if strings.HasPrefix(w.Name, seat+"-") {
			mine = append(mine, w)
		}
	}
	if len(mine) <= 1 {
		if len(mine) == 1 {
			return mine[0], true
		}
		return ReapWindow{}, false
	}
	if n.Seats == nil {
		return ReapWindow{}, false
	}
	start, err := n.Seats.SeatStart(ctx, seat, n.Host)
	if err != nil {
		return ReapWindow{}, false
	}
	var named []ReapWindow
	for _, w := range mine {
		if strings.Contains(start.Acting, w.Name) || strings.Contains(start.Acting, w.ID) {
			named = append(named, w)
		}
	}
	if len(named) == 1 {
		return named[0], true
	}
	return ReapWindow{}, false
}

func (n *EventNudge) say(what string, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.said == nil {
		n.said = map[string]string{}
	}
	if n.Err != nil && n.said[what] != err.Error() {
		fmt.Fprintf(n.Err, "mw events follow: telling %s: %v\n", what, err)
	}
	n.said[what] = err.Error()
}

func (n *EventNudge) quiet(what string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.said, what)
}

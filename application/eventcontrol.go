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

// RunCancelled is the run state a story carries once a cancel event ended its
// session: held, with its worktree kept, and recorded here so that mw status
// and the next attempt can tell it from a session that ended by itself.
const RunCancelled = "cancelled"

// GovernorPosternActor is the actor of an event made by the Governor's tap
// through the postern: a hold on a claimed story is a cancel by him.
const GovernorPosternActor = "governor@postern"

// ControlCursorKey is where EventControl keeps, in the follower's seat
// cursors, the last seq of the log it has acted on.
const ControlCursorKey = "control"

// HostPause is a pause-host event that no later resume-host has undone: the
// host does not start stories.
type HostPause struct {
	Seq   uint64
	At    time.Time
	Actor string
}

// PausedHost reports whether the log's latest pause-host or resume-host for
// host is a pause, and that pause. The log is read whole: a pause is rare and
// a pass reads the log once.
func PausedHost(ctx context.Context, log EventLog, host string) (HostPause, bool, error) {
	evs, err := log.Since(ctx, 0)
	if err != nil {
		return HostPause{}, false, fmt.Errorf("reading the event log for a pause of %s: %w", host, err)
	}
	var pause HostPause
	var paused bool
	for _, ev := range evs {
		c, ok := events.ControlOf(ev)
		if !ok || c.Host != host {
			continue
		}
		switch c.Word {
		case events.ControlPauseHost:
			pause, paused = HostPause{Seq: ev.Seq, At: ev.Ts, Actor: ev.Actor}, true
		case events.ControlResumeHost:
			pause, paused = HostPause{}, false
		}
	}
	return pause, paused, nil
}

// Cancel is a cancel event as mw status shows it.
type Cancel struct {
	Bead  string
	Actor string
	At    time.Time
}

// CancelsSince is the latest cancel event of each bead at or after since,
// oldest first.
func CancelsSince(ctx context.Context, log EventLog, since time.Time) ([]Cancel, error) {
	evs, err := log.Since(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("reading the event log for cancels: %w", err)
	}
	latest := map[string]int{}
	var cancels []Cancel
	for _, ev := range evs {
		if c, ok := events.ControlOf(ev); !ok || c.Word != events.ControlCancel || ev.Ts.Before(since) {
			continue
		}
		cancel := Cancel{Bead: ev.Bead, Actor: ev.Actor, At: ev.Ts}
		if i, seen := latest[ev.Bead]; seen {
			cancels[i] = cancel
			continue
		}
		latest[ev.Bead] = len(cancels)
		cancels = append(cancels, cancel)
	}
	return cancels, nil
}

// EventController is what EventFollow calls each pass to act on the control
// events in the log: EventControl.
type EventController interface {
	Control(ctx context.Context) error
}

// EventControl is how the follower acts on a cancel event (mw-jrx0s.16): for
// a story this host has claimed it ends the story's session as the reaper
// ends a window, gives the claim back, holds the story, records run=cancelled
// and writes "cancelled by <actor> at <time>" on the bead. The worktree and
// the branch are left as they are, for a Clerk or mw retry to settle. The
// other control words (pause-host, resume-host, cap, priority) are the
// subscribers' to hear and Dispatch's to read: this leaves them.
//
// A cancel is acted on once. A follower with no cursor starts at the log's
// head, so a cancel from before it began is history. A cancel of a story that
// is not claimed, that no longer exists, or that another host claimed is left
// and said; one whose session would not close is tried again on the next pass
// and the passes after it do not go on to later events.
type EventControl struct {
	Log     EventLog
	Cursors NudgeCursors
	Tracker WorkTracker
	Runner  Runner
	// Host is this host: a story claimed on another is that host's to cancel.
	Host string
	// Err is where what is left or failed is said, each once while it repeats.
	Err io.Writer

	mu   sync.Mutex
	said map[string]string
}

// Control acts on the cancel events since the cursor.
func (c *EventControl) Control(ctx context.Context) error {
	if c.Log == nil || c.Cursors == nil || c.Tracker == nil || c.Runner == nil {
		return fmt.Errorf("acting on control events needs a log, a cursor, a tracker and a runner")
	}
	head, err := c.Log.Head(ctx)
	if err != nil {
		return err
	}
	cursors, err := c.Cursors.Load(ctx)
	if err != nil {
		return err
	}
	cur, known := cursors[ControlCursorKey]
	if !known || head <= cur {
		if known {
			return nil
		}
		return c.save(ctx, cursors, head)
	}
	evs, err := c.Log.Since(ctx, cur)
	if err != nil {
		return err
	}
	last := cur
	for _, ev := range evs {
		if control, ok := events.ControlOf(ev); ok && control.Word == events.ControlCancel && !c.cancel(ctx, ev) {
			break
		}
		last = ev.Seq
	}
	if last == cur {
		return nil
	}
	return c.save(ctx, cursors, last)
}

func (c *EventControl) save(ctx context.Context, cursors map[string]uint64, seq uint64) error {
	if cursors == nil {
		cursors = map[string]uint64{}
	}
	cursors[ControlCursorKey] = seq
	return c.Cursors.Save(ctx, cursors)
}

// cancel acts on one cancel event, and reports false when a later pass should
// try it again: the session would not close, or the story could not be held.
// What it says it says itself.
func (c *EventControl) cancel(ctx context.Context, ev events.Event) bool {
	id := ev.Bead
	detail, err := c.Tracker.ShowStory(ctx, id)
	if err != nil {
		c.say("cancel:"+id, fmt.Errorf("the cancel of %s was left: reading it failed: %w", id, err))
		return true
	}
	if detail.Status != StatusInProgress {
		c.say("cancel:"+id, fmt.Errorf("the cancel of %s was left: it is %s, not claimed", id, detail.Status))
		return true
	}
	if host := detail.Merged().Host; host != "" && host != c.Host {
		c.say("cancel:"+id, fmt.Errorf("the cancel of %s was left: it is claimed on %s, and this is %s", id, host, c.Host))
		return true
	}

	// The session first, so that nothing more is written on the story's branch
	// or bead while it is being taken apart. Closing is what the reaper does to
	// a window: the pane is not asked, the worktree is not touched.
	session := SessionName(id)
	status, err := c.Runner.Status(ctx, session)
	if err != nil {
		c.say("cancel:"+id, fmt.Errorf("the session of %s could not be read: %w", id, err))
		return false
	}
	if status.State != StateGone {
		if err := c.Runner.Close(ctx, session); err != nil {
			c.say("cancel:"+id, fmt.Errorf("the session %s would not close: %w", session, err))
			return false
		}
	}
	c.quiet("cancel:" + id)

	note := fmt.Sprintf("cancelled by %s at %s", ev.Actor, ev.Ts.UTC().Format(time.RFC3339))
	if err := c.Tracker.SetStoryState(ctx, id, RunState, RunCancelled, note); err != nil {
		c.say("state:"+id, fmt.Errorf("%s could not be recorded as %s=%s: %w", id, RunState, RunCancelled, err))
	}
	// The claim is given back before the hold, which comes last: a release
	// leaves a story open, and a hold alone leaves it assigned, hidden from
	// every dispatcher the day it is released.
	if err := c.Tracker.ReleaseClaim(ctx, id); err != nil {
		c.say("release:"+id, fmt.Errorf("the claim on %s could not be given back: %w", id, err))
		note += " (its claim could not be given back: " + oneLine(err.Error()) + ")"
	}
	if err := c.Tracker.HoldStory(ctx, id); err != nil {
		c.say("hold:"+id, fmt.Errorf("%s could not be held after its session was cancelled: %w", id, err))
		return false
	}
	c.quiet("hold:" + id)
	if err := c.Tracker.CommentOnStory(ctx, id, note); err != nil {
		c.say("comment:"+id, fmt.Errorf("%s could not be commented on: %w", id, err))
	}
	return true
}

func (c *EventControl) say(what string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.said == nil {
		c.said = map[string]string{}
	}
	if c.Err != nil && c.said[what] != err.Error() {
		fmt.Fprintf(c.Err, "mw events follow: %s\n", strings.TrimSpace(err.Error()))
	}
	c.said[what] = err.Error()
}

func (c *EventControl) quiet(what string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.said, what)
}

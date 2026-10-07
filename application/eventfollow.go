package application

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// DefaultEventFollowEvery is how often EventFollow reads the beads' head: the
// Governor's tap shows within two seconds of the bead changing.
const DefaultEventFollowEvery = time.Second

// FollowWindow is how far before its cursor's time each pass of EventFollow
// reads the beads' changes again. bd stamps a change to the second, and with
// the clock of the host that made it, so a change stamped a little before
// the last one seen is still caught; one read twice is known by its key.
const FollowWindow = time.Minute

// BeadsHead is the cheapest read that says whether any bead has changed: a
// hash of the beads' database that differs after every write, and is the same
// while nothing is written. It is an opaque string: only equality means
// anything.
type BeadsHead interface {
	Head(ctx context.Context) (string, error)
}

// ChangeComment is BeadChange.What for a comment.
const ChangeComment = "comment"

// TypeMail is the bead type a message of the factory's mail is filed as.
const TypeMail = "mail"

// BeadNow is a bead as it stands: what its state in the bead machine is read
// from (BeadState).
type BeadNow struct {
	ID       string
	Type     string
	Status   string // bd's: open, deferred, in_progress, closed
	Run      string // its run state label's value (RunRunning, RunLanded, ...), if any
	Assignee string // for mail, its box: the seat it is sent to
}

// BeadChange is one change the beads record: a row of bd's own audit of a
// bead (created, updated, closed, label_added, ...) or a comment.
type BeadChange struct {
	// Key is the change's own id in the beads: a change read twice is one.
	Key   string
	At    time.Time
	Actor string
	// What is bd's word for the change, or ChangeComment.
	What string
	// Comment is a comment's text.
	Comment string
	// Bead is the bead changed, as it stands now, not as it stood then.
	Bead BeadNow
}

// BeadFeed is what EventFollow reads the beads through.
type BeadFeed interface {
	// BeadStates is every bead as it stands, and the time of the newest
	// change the beads hold: where a follower with no cursor starts.
	BeadStates(ctx context.Context) ([]BeadNow, time.Time, error)
	// BeadChanges is every change at or after since, oldest first.
	BeadChanges(ctx context.Context, since time.Time) ([]BeadChange, error)
}

// FollowCursor is how far EventFollow has turned the beads into events: the
// time of the newest change read, the keys of the changes read within
// FollowWindow of it, and every bead's state in the bead machine.
type FollowCursor struct {
	Since  time.Time         `json:"since"`
	Seen   []string          `json:"seen"`
	States map[string]string `json:"states"`
	// Verified is every bead a comment has marked verified (commentMarksVerified):
	// only the first such comment of a bead is the move to verified.
	Verified map[string]bool `json:"verified,omitempty"`
}

// FollowCursors keeps EventFollow's cursor between runs.
type FollowCursors interface {
	// Load is the cursor last saved, and false when none ever was.
	Load(ctx context.Context) (FollowCursor, bool, error)
	Save(ctx context.Context, c FollowCursor) error
}

// EventShipper sends the log's new events to the Governor: EventShip.
type EventShipper interface {
	Ship(ctx context.Context) error
}

// EventFollow is the home's follower, the one process that writes the beads'
// events: each pass reads the beads' head and, when it has moved, reads the
// changes since the cursor, turns each into events by kind, appends them to
// Log and saves the cursor; and republishes the live view, so a tap of the
// Governor's shows at once. A pass whose head is unchanged reads nothing else
// and costs no tokens. The first pass of a follower with no cursor only reads
// every bead's state: it appends nothing.
//
// A bead's status move is one bead_changed event per step of the bead
// machine between the state it was in and the one it is in (events.Path), so
// a claim and a start seen in one pass are two events. A new mail bead is one
// mail event, its box in Detail; any other change that left the state alone
// is a bead_changed event from and to that state, Detail bd's word for it.
// A comment beginning QUESTION is card_asked, ANSWER card_answered, RAN
// hands_ran and 'The Governor by postern' message; the first comment of a bead
// that begins VERIFIED (commentMarksVerified) is a bead_changed event from the
// bead's state to verified, Detail "verified", when the bead machine allows it;
// any other comment is a bead_changed event with Detail "comment".
//
// A failure — of the head read, the changes, the append or Publish — is said
// on Err and the loop goes on; what failed is tried again on the next pass,
// since the head it was made at is not recorded until it succeeds, and a
// failure that repeats word for word is said once. An event its machine
// forbids is said and left out. With no Log, it only republishes.
//
// With a Nudger, every pass calls it, whether or not the beads' head moved,
// to tell the seats of the events they subscribed to (EventNudge); with a
// Springer, that, to run the jobs those events call for (EventSpring); and
// with a Shipper it ends by calling that, to send the events written since its
// last batch and retry the batches waiting for the chain (EventShip). The
// Shipper is last because a send can take as long as a backend takes to fail.
// With a Controller it acts, before any of them, on the cancel events in the log.
type EventFollow struct {
	Head    BeadsHead
	Feed    BeadFeed
	Log     EventLog
	Cursors FollowCursors
	// Publish builds the view and writes it where the backend serves it.
	Publish func(ctx context.Context) error
	// Shipper, when set, is called at the end of each pass, after the
	// Springer, so a slow send holds nothing else back.
	Shipper EventShipper
	// Nudger, when set, is called each pass, before the Springer: it tells
	// the seats of their events.
	Nudger EventNudger
	// Springer, when set, is called each pass, after the Nudger: it starts
	// the jobs the pass's events, or the clock, call for (EventSpring).
	Springer EventSpringer
	// Trimmer, when set, is called at the end of the first pass of each UTC
	// day, after the Shipper, to move old events out of the log (EventTrim).
	Trimmer EventTrimmer
	// Controller, when set, is called each pass once the beads' events are
	// written and before anything is nudged, sprung or sent: it acts on the cancel events in the
	// log (EventControl), so a hold shows in seconds, not behind a slow send.
	Controller EventController
	// Aside publishes in a goroutine of its own, one publish at a time and
	// the newest head next, so a view that takes longer than a pass (25 s on
	// the Laptop on 2026-10-01) never holds up the events. Without it each
	// pass publishes in turn.
	Aside bool
	// Every is the wait between two passes; zero means
	// DefaultEventFollowEvery.
	Every time.Duration
	// Sleep waits d, returning early with the context's error when ctx ends;
	// a test replaces it. Nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
	// Err is where failures are said. A nil Err says nothing.
	Err io.Writer
	// Now is the follower's clock, which the cursor never passes: a change
	// stamped ahead by a host whose clock runs fast would otherwise move it
	// past changes still to come. Nil is time.Now.
	Now func() time.Time
}

// Run follows the beads until ctx ends, which is a clean stop: it returns nil.
func (f EventFollow) Run(ctx context.Context) error {
	if f.Head == nil || f.Publish == nil {
		return fmt.Errorf("mw events follow: no beads head or no view to publish")
	}
	if f.Log != nil && (f.Feed == nil || f.Cursors == nil) {
		return fmt.Errorf("mw events follow: an event log needs the beads' changes and a cursor")
	}
	every := f.Every
	if every <= 0 {
		every = DefaultEventFollowEvery
	}
	sleep := f.Sleep
	if sleep == nil {
		sleep = sleepFor
	}
	var sayMu sync.Mutex
	said := map[string]string{}
	say := func(what string, err error) {
		sayMu.Lock()
		defer sayMu.Unlock()
		if f.Err != nil && said[what] != err.Error() {
			fmt.Fprintf(f.Err, "mw events follow: %s: %v\n", what, err)
		}
		said[what] = err.Error()
	}
	quiet := func(what string) {
		sayMu.Lock()
		defer sayMu.Unlock()
		delete(said, what)
	}
	var published string
	publishAt := func(head string) {
		if head == published {
			return
		}
		if err := f.Publish(ctx); err != nil {
			if ctx.Err() == nil {
				say("publishing the view", err)
			}
			return
		}
		published = head
		quiet("publishing the view")
	}
	kick := make(chan string, 1)
	if f.Aside {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case head := <-kick:
					publishAt(head)
				}
			}
		}()
		defer wg.Wait()
	}
	var read, trimmedDay string
	var cursor *FollowCursor
	for ctx.Err() == nil {
		head, err := f.Head.Head(ctx)
		switch {
		case ctx.Err() != nil:
		case err != nil:
			say("reading the beads' head", err)
		default:
			quiet("reading the beads' head")
			if f.Log != nil && head != read {
				if err := f.pass(ctx, &cursor, say); err != nil {
					if ctx.Err() == nil {
						say("following the beads", err)
					}
				} else {
					read = head
					quiet("following the beads")
				}
			}
			if !f.Aside {
				publishAt(head)
				break
			}
			// Only this loop sends, so a kick drained here leaves room
			// for the newest head.
			select {
			case <-kick:
			default:
			}
			kick <- head
		}
		if f.Controller != nil && f.Log != nil && ctx.Err() == nil {
			if err := f.Controller.Control(ctx); err != nil {
				if ctx.Err() == nil {
					say("acting on control events", err)
				}
			} else {
				quiet("acting on control events")
			}
		}
		if f.Nudger != nil && f.Log != nil && ctx.Err() == nil {
			if err := f.Nudger.Nudge(ctx); err != nil {
				if ctx.Err() == nil {
					say("telling the seats", err)
				}
			} else {
				quiet("telling the seats")
			}
		}
		if f.Springer != nil && f.Log != nil && ctx.Err() == nil {
			if err := f.Springer.Spring(ctx); err != nil {
				if ctx.Err() == nil {
					say("springing the jobs", err)
				}
			} else {
				quiet("springing the jobs")
			}
		}
		// Ship goes last: a send that hangs or fails slowly (a 502 took ~26 s
		// on 2026-10-07) must not hold back a nudge or a spring.
		if f.Shipper != nil && ctx.Err() == nil {
			if err := f.Shipper.Ship(ctx); err != nil {
				if ctx.Err() == nil {
					say("sending events", err)
				}
			} else {
				quiet("sending events")
			}
		}
		if f.Trimmer != nil && ctx.Err() == nil {
			now := time.Now
			if f.Now != nil {
				now = f.Now
			}
			if day := now().UTC().Format("2006-01-02"); day != trimmedDay {
				if err := f.Trimmer.Trim(ctx); err != nil {
					if ctx.Err() == nil {
						say("trimming the log", err)
					}
				} else {
					trimmedDay = day
					quiet("trimming the log")
				}
			}
		}
		if sleep(ctx, every) != nil {
			break
		}
	}
	return nil
}

// pass turns the changes since *cursor into events, loading the cursor (or
// seeding it) the first time. *cursor moves only once the events are written.
func (f EventFollow) pass(ctx context.Context, cursor **FollowCursor, say func(string, error)) error {
	if *cursor == nil {
		c, ok, err := f.Cursors.Load(ctx)
		if err != nil {
			return fmt.Errorf("loading the cursor: %w", err)
		}
		if !ok {
			if c, err = f.seed(ctx); err != nil {
				return err
			}
			if err := f.Cursors.Save(ctx, c); err != nil {
				return fmt.Errorf("saving the cursor: %w", err)
			}
			*cursor = &c
			return nil
		}
		if c.States == nil {
			c.States = map[string]string{}
		}
		*cursor = &c
	}
	c := **cursor
	changes, err := f.Feed.BeadChanges(ctx, c.Since.Add(-FollowWindow))
	if err != nil {
		return fmt.Errorf("reading the beads' changes: %w", err)
	}
	seen := make(map[string]bool, len(c.Seen))
	for _, k := range c.Seen {
		seen[k] = true
	}
	next := FollowCursor{Since: c.Since, States: make(map[string]string, len(c.States))}
	for id, s := range c.States {
		next.States[id] = s
	}
	next.Verified = make(map[string]bool, len(c.Verified))
	for id := range c.Verified {
		next.Verified[id] = true
	}
	var fresh []BeadChange
	for _, ch := range changes {
		if ch.At.After(next.Since) {
			next.Since = ch.At
		}
		if !seen[ch.Key] {
			fresh = append(fresh, ch)
		}
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	if t := now(); next.Since.After(t) {
		next.Since = c.Since
		if t.After(c.Since) {
			next.Since = t
		}
	}
	for _, ch := range changes {
		if !ch.At.Before(next.Since.Add(-FollowWindow)) {
			next.Seen = append(next.Seen, ch.Key)
		}
	}
	if len(fresh) == 0 && next.Since.Equal(c.Since) {
		return nil
	}
	var evs []events.Event
	for _, e := range eventsOf(fresh, next.States, next.Verified) {
		check := e
		check.Seq = 1
		if err := check.Validate(); err != nil {
			say("leaving out an event", fmt.Errorf("%s on %s: %w", e.Kind, e.Bead, err))
			continue
		}
		evs = append(evs, e)
	}
	if len(evs) > 0 {
		if _, err := f.Log.Append(ctx, evs); err != nil {
			return fmt.Errorf("appending %d events: %w", len(evs), err)
		}
	}
	if err := f.Cursors.Save(ctx, next); err != nil {
		return fmt.Errorf("saving the cursor: %w", err)
	}
	*cursor = &next
	return nil
}

// seed is the cursor of a follower that has none: every bead's state now,
// since the newest change, with the changes up to then already seen. A change
// stamped after the newest came in between the two reads, so it is left to
// the next pass.
func (f EventFollow) seed(ctx context.Context) (FollowCursor, error) {
	beads, newest, err := f.Feed.BeadStates(ctx)
	if err != nil {
		return FollowCursor{}, fmt.Errorf("reading every bead's state: %w", err)
	}
	c := FollowCursor{Since: newest, States: make(map[string]string, len(beads))}
	for _, b := range beads {
		c.States[b.ID] = BeadState(b)
	}
	changes, err := f.Feed.BeadChanges(ctx, newest.Add(-FollowWindow))
	if err != nil {
		return FollowCursor{}, fmt.Errorf("reading the beads' changes: %w", err)
	}
	for _, ch := range changes {
		if !ch.At.After(newest) {
			c.Seen = append(c.Seen, ch.Key)
		}
	}
	return c, nil
}

// BeadState is b's state in the bead machine: held, open or closed from its
// status, and an in-progress bead's from its run state — landed, refused
// (blocked, stopped or stuck), running, and claimed before a session starts.
func BeadState(b BeadNow) string {
	switch b.Status {
	case StatusHeld:
		return events.BeadHeld
	case StatusClosed:
		return events.BeadClosed
	case StatusInProgress:
		switch b.Run {
		case RunLanded:
			return events.BeadLanded
		case RunBlocked, RunStopped, RunStuck:
			return events.BeadRefused
		case RunRunning:
			return events.BeadRunning
		}
		return events.BeadClaimed
	}
	return events.BeadOpen
}

// eventsOf turns changes, oldest first, into events with no seq: each bead's
// own changes first, at most one group of events per bead, then the comments.
// states is every bead's state before, and is moved on to after; verified is
// the beads a comment has already marked verified, and gains each one that does.
func eventsOf(changes []BeadChange, states map[string]string, verified map[string]bool) []events.Event {
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].At.Before(changes[j].At) })
	event := func(ch BeadChange, kind, from, to, detail string) events.Event {
		return events.Event{Ts: ch.At.UTC(), Kind: kind, Bead: ch.Bead.ID, Actor: ch.Actor, From: from, To: to, Detail: detail, Lane: events.LaneNormal}
	}
	var evs []events.Event
	done := map[string]bool{}
	for _, ch := range changes {
		id := ch.Bead.ID
		if ch.What == ChangeComment || done[id] {
			continue
		}
		done[id] = true
		now := BeadState(ch.Bead)
		was, known := states[id]
		states[id] = now
		switch {
		case !known && ch.Bead.Type == TypeMail:
			evs = append(evs, event(ch, events.KindMail, "", "", ch.Bead.Assignee))
		case known && was == now:
			evs = append(evs, event(ch, events.KindBeadChanged, now, now, ch.What))
		default:
			from := was
			if !known {
				from = events.Start
			}
			for _, to := range events.Path(events.MachineBead, from, now) {
				evs = append(evs, event(ch, events.KindBeadChanged, from, to, "status"))
				from = to
			}
		}
	}
	for _, ch := range changes {
		if ch.What != ChangeComment {
			continue
		}
		state, known := states[ch.Bead.ID]
		if !known {
			state = BeadState(ch.Bead)
			states[ch.Bead.ID] = state
		}
		text := ch.Comment
		switch {
		case strings.HasPrefix(text, "QUESTION "):
			evs = append(evs, event(ch, events.KindCardAsked, events.Start, events.CardAsked, commentTxid(text)))
		case strings.HasPrefix(text, "ANSWER "):
			evs = append(evs, event(ch, events.KindCardAnswered, events.CardAsked, events.CardAnswered, commentTxid(text)))
		case strings.HasPrefix(text, "RAN "):
			step := ""
			if m := ranStep.FindStringSubmatch(text); m != nil {
				step = m[1]
			}
			evs = append(evs, event(ch, events.KindHandsRan, "", "", step))
		case strings.HasPrefix(text, "The Governor by postern"):
			evs = append(evs, event(ch, events.KindMessage, "", "", commentTxid(text)))
		case commentMarksVerified(text) && !verified[ch.Bead.ID] && events.Transition(events.MachineBead, state, events.BeadVerified) == nil:
			verified[ch.Bead.ID] = true
			evs = append(evs, event(ch, events.KindBeadChanged, state, events.BeadVerified, "verified"))
		default:
			evs = append(evs, event(ch, events.KindBeadChanged, state, state, ChangeComment))
		}
	}
	return evs
}

// txidInComment is the txid a postern comment names: "txid direct:<hex>" or
// a chain txid, ended by the colon, comma or bracket after it.
var txidInComment = regexp.MustCompile(`txid ((?:[a-z]+:)?[0-9A-Za-z]+)`)

// ranStep is the step a RAN comment says ran (posternrun.go).
var ranStep = regexp.MustCompile(`^RAN step (\S+)`)

// commentTxid is the first txid text names, "" when it names none.
func commentTxid(text string) string {
	if m := txidInComment.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// sleepFor waits d or until ctx ends, whichever is first, returning the
// context's error in the second case.
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

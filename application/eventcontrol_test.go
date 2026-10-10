package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// controlWorld is a follower's control pass over fakes: one story claimed by
// this host with a session running, one claimed by the desktop, one open.
type controlWorld struct {
	log     *apptest.FakeEventLog
	cursors *apptest.FakeNudgeCursors
	tracker *apptest.FakeTracker
	runner  *apptest.FakeRunner
	err     bytes.Buffer
	control *application.EventControl
}

func newControlWorld(t *testing.T) *controlWorld {
	t.Helper()
	w := &controlWorld{log: &apptest.FakeEventLog{}, cursors: &apptest.FakeNudgeCursors{}, tracker: apptest.NewFakeTracker(), runner: apptest.NewFakeRunner()}
	w.tracker.AddEpic("mw-e", domain.Path{Rig: "millwright", Branch: "main", Host: "laptop"})
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.3"} {
		w.tracker.AddStory("mw-e", domain.Story{ID: id, Title: "Story " + id})
	}
	w.tracker.AddStory("mw-e", domain.Story{ID: "mw-e.4", Title: "Story mw-e.4", Overrides: domain.Path{Host: "desktop"}})
	mustDo(t, w.tracker.ClaimStory(context.Background(), "mw-e.1"))
	mustDo(t, w.tracker.SetStoryState(context.Background(), "mw-e.1", application.RunState, application.RunRunning, ""))
	mustDo(t, w.tracker.ClaimAs("mw-e.4", "mw@desktop", time.Now().Add(time.Hour)))
	for _, id := range []string{"mw-e.1", "mw-e.4"} {
		mustDo(t, w.runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName(id), Command: []string{"claude"}}))
	}
	w.control = &application.EventControl{
		Log: w.log, Cursors: w.cursors, Tracker: w.tracker, Runner: w.runner, Host: "laptop", Err: &w.err,
	}
	return w
}

func (w *controlWorld) pass(t *testing.T) {
	t.Helper()
	if err := w.control.Control(context.Background()); err != nil {
		t.Fatalf("control: %v", err)
	}
}

func (w *controlWorld) detail(t *testing.T, id string) application.StoryDetail {
	t.Helper()
	d, err := w.tracker.ShowStory(context.Background(), id)
	mustDo(t, err)
	return d
}

func cancelOf(bead, actor string) events.Event {
	return events.Event{Kind: events.KindControl, Bead: bead, Actor: actor, Detail: events.ControlCancel}
}

// A cancel on a story this host has claimed ends its session, gives the claim
// back, holds the story, records run=cancelled, and writes who and when on
// the bead; a second pass does nothing more.
func TestCancelEndsTheSessionAndHoldsTheStoryWithANote(t *testing.T) {
	w := newControlWorld(t)
	w.pass(t) // starts at the head: nothing is acted on
	appendAll(t, w.log, cancelOf("mw-e.1", "governor@postern"))
	w.pass(t)

	if got := w.runner.Closed(); len(got) != 1 || got[0] != application.SessionName("mw-e.1") {
		t.Fatalf("closed %v, want the session of mw-e.1 alone", got)
	}
	if st, _ := w.runner.Status(context.Background(), application.SessionName("mw-e.1")); st.State != application.StateGone {
		t.Fatalf("the session is %s, want gone", st.State)
	}
	d := w.detail(t, "mw-e.1")
	if d.Status != apptest.StatusDeferred || d.Assignee != "" {
		t.Fatalf("the story is %s assigned to %q, want held and unassigned", d.Status, d.Assignee)
	}
	if run, _ := w.tracker.StoryState(context.Background(), "mw-e.1", application.RunState); run != application.RunCancelled {
		t.Fatalf("run state %q, want %q", run, application.RunCancelled)
	}
	if got := w.tracker.Comments("mw-e.1"); len(got) != 1 || got[0] != "cancelled by governor@postern at 2026-10-01T12:00:00Z" {
		t.Fatalf("comments %q, want the cancelled-by note", got)
	}
	w.pass(t)
	if got := w.runner.Closed(); len(got) != 1 {
		t.Fatalf("a second pass closed again: %v", got)
	}
	if got := w.tracker.Comments("mw-e.1"); len(got) != 1 {
		t.Fatalf("a second pass commented again: %q", got)
	}
}

// A cancel written before the follower had a cursor is history, and is not
// acted on: a fresh attempt of the story is never killed by last week's.
func TestCancelFromBeforeTheFirstPassIsHistory(t *testing.T) {
	w := newControlWorld(t)
	appendAll(t, w.log, cancelOf("mw-e.1", "governor@postern"))
	w.pass(t)
	w.pass(t)
	if got := w.runner.Closed(); len(got) != 0 {
		t.Fatalf("closed %v from history", got)
	}
	if d := w.detail(t, "mw-e.1"); d.Status != apptest.StatusInProgress {
		t.Fatalf("the story is %s", d.Status)
	}
}

// A story another host holds is that host's to cancel: this one touches
// neither its session nor its bead, and says so.
func TestCancelOfAnotherHostsStoryIsLeftAlone(t *testing.T) {
	w := newControlWorld(t)
	w.pass(t)
	appendAll(t, w.log, cancelOf("mw-e.4", "governor@postern"))
	w.pass(t)
	if got := w.runner.Closed(); len(got) != 0 {
		t.Fatalf("closed %v", got)
	}
	if d := w.detail(t, "mw-e.4"); d.Status != apptest.StatusInProgress || d.Assignee != "mw@desktop" {
		t.Fatalf("the desktop's story is %s held by %q", d.Status, d.Assignee)
	}
	if !strings.Contains(w.err.String(), "mw-e.4") || !strings.Contains(w.err.String(), "desktop") {
		t.Fatalf("said %q, want it to name the story and the host", w.err.String())
	}
}

// A cancel of a story nobody has claimed, or that no longer exists, changes
// nothing.
func TestCancelOfAStoryNotClaimedChangesNothing(t *testing.T) {
	w := newControlWorld(t)
	w.pass(t)
	appendAll(t, w.log, cancelOf("mw-e.2", "governor@postern"), cancelOf("mw-nope", "governor@postern"))
	w.pass(t)
	if got := w.runner.Closed(); len(got) != 0 {
		t.Fatalf("closed %v", got)
	}
	if d := w.detail(t, "mw-e.2"); d.Status != apptest.StatusOpen {
		t.Fatalf("the open story is %s", d.Status)
	}
	if got := w.tracker.Comments("mw-e.2"); len(got) != 0 {
		t.Fatalf("commented on an unclaimed story: %q", got)
	}
}

// The other control words are for the seats that subscribe to them: the
// follower's cancel pass leaves them, and the claims, alone.
func TestOtherControlWordsAreNotCancels(t *testing.T) {
	w := newControlWorld(t)
	w.pass(t)
	appendAll(t, w.log,
		events.Event{Kind: events.KindControl, Actor: "mw@laptop", Detail: "pause-host laptop"},
		events.Event{Kind: events.KindControl, Bead: "mw-e.1", Actor: "mw@laptop", Detail: "priority 0"},
		events.Event{Kind: events.KindControl, Actor: "mw@laptop", Detail: "cap laptop 1"})
	w.pass(t)
	if got := w.runner.Closed(); len(got) != 0 {
		t.Fatalf("closed %v", got)
	}
	if d := w.detail(t, "mw-e.1"); d.Status != apptest.StatusInProgress {
		t.Fatalf("the story is %s", d.Status)
	}
}

// A session that will not close leaves the cancel to the next pass, and is
// said once.
func TestCancelIsTriedAgainWhenTheSessionWillNotClose(t *testing.T) {
	w := newControlWorld(t)
	w.pass(t)
	appendAll(t, w.log, cancelOf("mw-e.1", "governor@postern"))
	w.runner.Err = errors.New("tmux is wedged")
	w.pass(t)
	w.pass(t)
	if d := w.detail(t, "mw-e.1"); d.Status != apptest.StatusInProgress {
		t.Fatalf("the story is %s though its session was not closed", d.Status)
	}
	if n := strings.Count(w.err.String(), "tmux is wedged"); n != 1 {
		t.Fatalf("said %d times: %q", n, w.err.String())
	}
	w.runner.Err = nil
	w.pass(t)
	if d := w.detail(t, "mw-e.1"); d.Status != apptest.StatusDeferred {
		t.Fatalf("the retry left the story %s", d.Status)
	}
}

// EventFollow calls its Controller each pass, before it sends events.
func TestFollowCallsTheControllerEveryPass(t *testing.T) {
	var passes int
	ctx, cancel := context.WithCancel(context.Background())
	f := application.EventFollow{
		Head:       &apptest.FakeHead{},
		Publish:    func(context.Context) error { return nil },
		Log:        &apptest.FakeEventLog{},
		Feed:       apptest.NewFakeTracker(),
		Cursors:    &apptest.FakeFollowCursors{},
		Controller: controllerFunc(func(context.Context) error { passes++; return nil }),
		Sleep: func(context.Context, time.Duration) error {
			if passes >= 3 {
				cancel()
				return context.Canceled
			}
			return nil
		},
	}
	if err := f.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if passes != 3 {
		t.Fatalf("the controller ran %d times, want 3", passes)
	}
}

type controllerFunc func(context.Context) error

func (c controllerFunc) Control(ctx context.Context) error { return c(ctx) }

// A pause-host in the home's log is mirrored by the follower's control pass to
// the host.<h>.paused note, and a resume-host clears it, so that a Boost, which
// reads another log, can see the pause (mw-sgtc6p).
func TestHomePauseSetsTheNoteAndResumeClearsIt(t *testing.T) {
	ctx := context.Background()
	w := newControlWorld(t)
	w.control.Notes = w.tracker
	w.pass(t)
	if got, _ := w.tracker.Note(ctx, application.PausedKey("boost")); got != "" {
		t.Fatalf("a note before any pause: %q", got)
	}

	appendAll(t, w.log, control("governor@postern", "pause-host boost"))
	w.pass(t)
	got, _ := w.tracker.Note(ctx, application.PausedKey("boost"))
	if got == "" {
		t.Fatal("the pause did not set the note")
	}
	if other, _ := w.tracker.Note(ctx, application.PausedKey("laptop")); other != "" {
		t.Fatalf("a pause of boost set laptop's note: %q", other)
	}

	appendAll(t, w.log, control("governor@postern", "resume-host boost"))
	w.pass(t)
	if got, _ := w.tracker.Note(ctx, application.PausedKey("boost")); got != "" {
		t.Fatalf("the resume left the note %q", got)
	}
}

// A follower that starts with a pause already in the log mirrors it too, and a
// pause then resume in one pass leaves no note.
func TestFirstPassMirrorsAPauseAlreadyInTheLog(t *testing.T) {
	ctx := context.Background()
	w := newControlWorld(t)
	w.control.Notes = w.tracker
	appendAll(t, w.log,
		control("governor@postern", "pause-host boost"),
		control("governor@postern", "pause-host laptop"),
		control("governor@postern", "resume-host laptop"))
	w.pass(t)
	if got, _ := w.tracker.Note(ctx, application.PausedKey("boost")); got == "" {
		t.Fatal("a pause from before the follower began was not mirrored")
	}
	if got, _ := w.tracker.Note(ctx, application.PausedKey("laptop")); got != "" {
		t.Fatalf("a pause already resumed left the note %q", got)
	}
}

// A note that cannot be written is tried again on the next pass.
func TestPauseNoteIsTriedAgainWhenItCannotBeWritten(t *testing.T) {
	ctx := context.Background()
	w := newControlWorld(t)
	w.control.Notes = w.tracker
	w.pass(t)
	appendAll(t, w.log, control("governor@postern", "pause-host boost"))
	w.tracker.Err = errors.New("dolt is down")
	w.pass(t)
	w.tracker.Err = nil
	w.pass(t)
	if got, _ := w.tracker.Note(ctx, application.PausedKey("boost")); got == "" {
		t.Fatal("the pause note was not written once the tracker came back")
	}
}

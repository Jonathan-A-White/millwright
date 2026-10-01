package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// The Governor's hold tap on a story a session has claimed appends a cancel
// event for the follower to act on, says so on the bead and to the Mayor, and
// leaves the bead's status to the follower: the session is still to be ended.
func TestApplyHoldOfAClaimedStoryEmitsACancelEvent(t *testing.T) {
	f := newApplyFixture(t)
	log := &apptest.FakeEventLog{}
	f.action(t, "tx-claimed", map[string]any{"action": "hold", "bead": "mw-e.4"})

	inbox := f.inbox()
	inbox.Events = log
	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}

	got := log.All()
	if len(got) != 1 {
		t.Fatalf("the log holds %d events, want one cancel: %+v", len(got), got)
	}
	ev := got[0]
	if ev.Kind != events.KindControl || ev.Bead != "mw-e.4" || ev.Detail != events.ControlCancel || ev.Actor != application.GovernorPosternActor || ev.Lane != events.LaneNormal {
		t.Fatalf("the event is %+v, want a normal-lane control cancel of mw-e.4 by %s", ev, application.GovernorPosternActor)
	}
	comments := f.tracker.Comments("mw-e.4")
	if len(comments) != 1 || !strings.HasPrefix(comments[0], "HELD by the Governor via postern, txid tx-claimed") || !strings.Contains(comments[0], "cancel") {
		t.Fatalf("comments %q, want the HELD comment saying the session is cancelled", comments)
	}
	if got := f.status(t, "mw-e.4"); got != apptest.StatusInProgress {
		t.Fatalf("the status is %s: ending the session and holding is the follower's", got)
	}
	if subjects := f.subjects(t); len(subjects) != 1 || subjects[0] != "Held: mw-e.4" {
		t.Fatalf("mail subjects %v, want Held: mw-e.4", subjects)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-claimed")); note != "applied hold mw-e.4 txid tx-claimed" {
		t.Fatalf("the txid was marked %q", note)
	}
}

// A hold of an open, unclaimed story emits nothing, and a cancel that cannot
// be written is a refusal the Mayor is told of, not a hold that did not happen
// in silence.
func TestApplyHoldEmitsNothingForAnOpenStoryAndRefusesWhenTheCancelCannotBeWritten(t *testing.T) {
	f := newApplyFixture(t)
	log := &apptest.FakeEventLog{}
	f.action(t, "tx-open", map[string]any{"action": "hold", "bead": "mw-e.3"})
	f.action(t, "tx-claimed", map[string]any{"action": "hold", "bead": "mw-e.4"})
	log.FailNext(errors.New("disk full"))

	inbox := f.inbox()
	inbox.Events = log
	// The open story's hold goes first and writes no event, so the failure is
	// the claimed one's.
	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if got := f.status(t, "mw-e.3"); got != apptest.StatusDeferred {
		t.Fatalf("the open story is %s, want held", got)
	}
	if got := log.All(); len(got) != 0 {
		t.Fatalf("events written: %+v", got)
	}
	if subjects := f.subjects(t); len(subjects) != 2 || subjects[1] != "Not applied: hold mw-e.4" {
		t.Fatalf("mail subjects %v", subjects)
	}
	if got := f.tracker.Comments("mw-e.4"); len(got) != 0 {
		t.Fatalf("commented on a refused hold: %q", got)
	}
}

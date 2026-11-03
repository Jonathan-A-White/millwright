package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// stallWorld is the follower-stalled check over a log whose newest bead event
// is at t0, a view written at viewAt, and beads whose changes the test adds.
type stallWorld struct {
	t0      time.Time
	tracker *apptest.FakeTracker
	log     *apptest.FakeEventLog
	viewAt  time.Time
	up      bool
	check   *application.FollowerStallCheck
}

func newStallWorld(t *testing.T) *stallWorld {
	t.Helper()
	w := &stallWorld{
		t0:      time.Date(2026, 11, 3, 0, 10, 34, 0, time.UTC),
		tracker: apptest.NewFakeTracker(),
		log:     &apptest.FakeEventLog{},
		up:      true,
	}
	w.viewAt = w.t0.Add(42 * time.Second)
	if _, err := w.log.Append(context.Background(), []events.Event{{
		Ts: w.t0, Kind: events.KindBeadChanged, Bead: "mw-1", Actor: "a", From: "open", To: "open", Lane: events.LaneNormal,
	}}); err != nil {
		t.Fatal(err)
	}
	w.check = &application.FollowerStallCheck{
		Up:          func(context.Context) (bool, error) { return w.up, nil },
		Log:         w.log,
		Feed:        w.tracker,
		ViewWritten: func(context.Context) (time.Time, bool, error) { return w.viewAt, true, nil },
		Now:         func() time.Time { return w.t0.Add(30 * time.Minute) },
	}
	return w
}

// change is a bead change min minutes after the newest event.
func (w *stallWorld) change(key string, min int) {
	w.tracker.AddBeadChange(application.BeadChange{
		Key: key, At: w.t0.Add(time.Duration(min) * time.Minute), Actor: "mw@laptop", What: "updated",
		Bead: application.BeadNow{ID: "mw-2", Type: "task", Status: application.StatusOpen},
	})
}

func TestFollowerStalledIsOkWhenTheEventsKeepUpWithTheBeads(t *testing.T) {
	w := newStallWorld(t)
	w.change("c1", 2)
	w.viewAt = w.t0.Add(2 * time.Minute)
	if v, why := w.check.Probe(context.Background()); v != application.DoctorOK {
		t.Fatalf("a follower 2 minutes behind is %v: %s", v, why)
	}
}

func TestFollowerStalledSaysSoWhenTheNewestBeadChangeIsOverFiveMinutesPastTheNewestEvent(t *testing.T) {
	w := newStallWorld(t)
	w.change("c1", 12)
	w.viewAt = w.t0.Add(12 * time.Minute) // the view is not the trouble
	v, why := w.check.Probe(context.Background())
	if v != application.DoctorCannotTell || !strings.Contains(why, "STALLED") || !strings.Contains(why, "over 10 minutes past the newest bead or mail event") {
		t.Fatalf("a follower 12 minutes behind is %v: %q", v, why)
	}
	if strings.Contains(why, "the view was last written") {
		t.Fatalf("the view was written 0 minutes before the change, and the reason blames it: %q", why)
	}
}

func TestFollowerStalledSaysSoWhenTheViewIsOverFiveMinutesOlderThanTheBeadsAndNotNewer(t *testing.T) {
	w := newStallWorld(t)
	w.change("c1", 4) // events within 5 minutes
	w.viewAt = w.t0.Add(-4 * time.Minute)
	v, why := w.check.Probe(context.Background())
	if v != application.DoctorCannotTell || !strings.Contains(why, "the view was last written") || strings.Contains(why, "past the newest bead or mail event") {
		t.Fatalf("a view 8 minutes behind the beads is %v: %q", v, why)
	}
}

func TestFollowerStalledSaysNothingOfAQuietBeadsOrADownFollower(t *testing.T) {
	w := newStallWorld(t)
	w.viewAt = w.t0.Add(-3 * time.Hour)
	if v, why := w.check.Probe(context.Background()); v != application.DoctorOK {
		t.Fatalf("beads with no change since the newest event are %v: %s", v, why)
	}
	w.change("c1", 20)
	w.up = false
	if v, why := w.check.Probe(context.Background()); v != application.DoctorOK || !strings.Contains(why, "does not run") {
		t.Fatalf("a follower that is not up here is %v: %q", v, why)
	}
}

func TestFollowerStalledReadsTheSameWhileTheStallGrowsByAMinute(t *testing.T) {
	w := newStallWorld(t)
	w.viewAt = w.t0.Add(21 * time.Minute)
	w.change("c1", 21)
	_, a := w.check.Probe(context.Background())
	w.change("c2", 24)
	w.viewAt = w.t0.Add(24 * time.Minute)
	_, b := w.check.Probe(context.Background())
	if a != b {
		t.Fatalf("the reason moved with the minute, so the note would be rewritten each run:\n%q\n%q", a, b)
	}
}

package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// limitedEvents is the seat_limited events the log holds.
func limitedEvents(t *testing.T, log application.EventLog) []events.SeatLimited {
	t.Helper()
	all, err := log.Since(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []events.SeatLimited
	for _, ev := range all {
		if ev.Kind != events.KindSeatLimited {
			continue
		}
		l, ok := events.SeatLimitedOf(ev)
		if !ok {
			t.Fatalf("a seat_limited event with an unreadable detail %q", ev.Detail)
		}
		out = append(out, l)
	}
	return out
}

// A limited pane found by the reaper is written once as a seat_limited event
// with the seat, the window and the reset time; a second look at the same
// limit, by this reaper or by another, writes none (mw-gq6.520).
func TestAReaperFindingALimitedPaneWritesOneSeatLimitedEventPerLimit(t *testing.T) {
	windows, files := aFinishedWindow(t)
	if err := windows.Shows("@3", aLimitScreen); err != nil {
		t.Fatal(err)
	}
	log := &apptest.FakeEventLog{}
	clock := &aClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	reap := func() {
		t.Helper()
		_, err := application.SeatReap{
			Seats: files, Terminal: windows, Log: &memoryLog{}, Events: log,
			Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
			Interval: time.Second, Limit: time.Minute,
			Now: clock.Now, Sleep: clock.Sleep,
		}.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}

	reap()
	got := limitedEvents(t, log)
	if len(got) != 1 {
		t.Fatalf("expected one seat_limited event after the first look, got %+v", got)
	}
	want := time.Date(2026, 10, 16, 7, 0, 0, 0, time.UTC)
	if got[0].Seat != "mayor" || got[0].Window != "mayor-2026-09-19-12" || !got[0].Resets.Equal(want) {
		t.Errorf("expected the seat mayor, its window mayor-2026-09-19-12 and the reset %s, got %+v", want, got[0])
	}

	reap()
	if got := limitedEvents(t, log); len(got) != 1 {
		t.Errorf("expected no second event for the same reset time, got %+v", got)
	}
}

// A new limit, with another reset time, is another episode and is written.
func TestAnotherResetTimeIsAnotherSeatLimitedEvent(t *testing.T) {
	log := &apptest.FakeEventLog{}
	now := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)
	first := application.PaneLimit{Notice: "You've hit your weekly limit · resets 7am", Resets: now.Add(time.Hour)}
	next := application.PaneLimit{Notice: "You've hit your session limit · resets 9am", Resets: now.Add(3 * time.Hour)}
	window := application.ReapWindow{ID: "@3", Name: "deputy-1"}

	for _, limit := range []application.PaneLimit{first, first, next, next} {
		if err := application.TellSeatLimited(context.Background(), log, func() time.Time { return now }, "deputy", "laptop", window, limit); err != nil {
			t.Fatal(err)
		}
	}

	if got := limitedEvents(t, log); len(got) != 2 || !got[1].Resets.Equal(next.Resets) {
		t.Errorf("expected one event for each of the two reset times, got %+v", got)
	}
}

// mw events tail shows the kind, with the seat, window and reset time.
func TestEventsTailShowsASeatLimitedEvent(t *testing.T) {
	log := &apptest.FakeEventLog{}
	now := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)
	limit := application.PaneLimit{Notice: "You've hit your weekly limit · resets 7am", Resets: now.Add(time.Hour)}
	if err := application.TellSeatLimited(context.Background(), log, func() time.Time { return now }, "deputy", "laptop", application.ReapWindow{ID: "@3", Name: "deputy-1"}, limit); err != nil {
		t.Fatal(err)
	}
	all, err := log.Since(context.Background(), 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("expected one event, got %v (%v)", all, err)
	}

	got := application.EventLine(all[0])

	want := "1 2026-10-16T06:00:00Z seat_limited - deputy@laptop - deputy in deputy-1 until 2026-10-16T07:00:00Z: You've hit your weekly limit · resets 7am"
	if got != want {
		t.Errorf("expected the tail line\n%s\ngot\n%s", want, got)
	}
}

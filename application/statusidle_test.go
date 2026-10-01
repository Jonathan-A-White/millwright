package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

type fixedHarness struct {
	n   int
	err error
}

func (f fixedHarness) Count(context.Context) (int, error) { return f.n, f.err }

// idleStatus is mw status read at 14:00 over a log of events, each at the
// time given, with the factory called idle after ten minutes.
func idleStatus(t *testing.T, harness application.HarnessCount, evs ...events.Event) string {
	t.Helper()
	log := &apptest.FakeEventLog{}
	for _, e := range evs {
		e.Lane = events.LaneNormal
		if _, err := log.Append(context.Background(), []events.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	tracker := apptest.NewFakeTracker()
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
		Log: log, IdleAfter: 10 * time.Minute, Harness: harness,
		Now: func() time.Time { return time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC) },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report.String()
}

func atTime(hhmm string, e events.Event) events.Event {
	ts, err := time.Parse("2006-01-02 15:04", "2026-10-01 "+hhmm)
	if err != nil {
		panic(err)
	}
	e.Ts = ts
	return e
}

func job(hhmm, actor, from, to string) events.Event {
	return atTime(hhmm, events.Event{Kind: events.KindJob, Actor: actor, From: from, To: to})
}

func landed(hhmm string) events.Event {
	return atTime(hhmm, events.Event{Kind: events.KindBeadChanged, Bead: "mw-a", Actor: "mw", From: events.BeadRunning, To: events.BeadLanded})
}

func TestStatusSaysIdleSinceTheLastEventThatIsNoJobsWhenOnlyJobsHaveRunForTheLimit(t *testing.T) {
	out := idleStatus(t, fixedHarness{n: 0},
		landed("13:20"),
		job("13:30", "dispatch@laptop", events.Start, events.JobScheduled),
		job("13:30", "dispatch@laptop", events.JobScheduled, events.JobRunning),
		job("13:31", "dispatch@laptop", events.JobRunning, events.JobDone),
		job("13:59", "dispatch@laptop", events.JobDone, events.JobScheduled),
		job("13:59", "dispatch@laptop", events.JobScheduled, events.JobRunning),
		job("13:59", "dispatch@laptop", events.JobRunning, events.JobDone),
	)
	if !strings.Contains(out, "IDLE since 13:20 · 0 harness processes\n") {
		t.Fatalf("expected the idle line in:\n%s", out)
	}
}

func TestStatusSaysNothingIdleWhileAnEventIsRecentOrAJobIsStillRunning(t *testing.T) {
	recent := idleStatus(t, fixedHarness{n: 1}, landed("13:55"))
	if strings.Contains(recent, "IDLE") || !strings.Contains(recent, "HARNESS 1 processes\n") {
		t.Fatalf("a landing five minutes ago is not idle, and the harness count shows:\n%s", recent)
	}
	running := idleStatus(t, fixedHarness{n: 0},
		landed("13:00"),
		job("13:10", "dispatch@laptop", events.Start, events.JobScheduled),
		job("13:10", "dispatch@laptop", events.JobScheduled, events.JobRunning),
	)
	if strings.Contains(running, "IDLE") {
		t.Fatalf("a job still running is not idle:\n%s", running)
	}
}

func TestStatusIdleLineSurvivesAHarnessCountItCannotRead(t *testing.T) {
	out := idleStatus(t, fixedHarness{err: errors.New("no /proc")}, landed("13:00"))
	if !strings.Contains(out, "IDLE since 13:00\n") {
		t.Fatalf("expected the idle line without a count in:\n%s", out)
	}
}

func TestStatusWithNoLogNamesNoIdleLine(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder"}.Run(context.Background())
	if err != nil || strings.Contains(report.String(), "IDLE") {
		t.Fatalf("a report with no log has no IDLE line: %v\n%s", err, report.String())
	}
}

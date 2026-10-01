package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

func TestEmitAppendsAJobEventStampedNow(t *testing.T) {
	log := &apptest.FakeEventLog{}
	now := time.Date(2026, 10, 1, 13, 5, 0, 0, time.UTC)
	got, err := application.EventEmit{
		Log: log,
		Now: func() time.Time { return now },
		Event: events.Event{
			Kind: events.KindJob, Bead: "mw-1", Actor: "dispatch@laptop",
			From: events.JobScheduled, To: events.JobRunning,
		},
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	all := log.All()
	if len(all) != 1 || got.Seq != 1 || all[0].Seq != 1 {
		t.Fatalf("expected one event at seq 1, emit returned %+v and the log holds %+v", got, all)
	}
	e := all[0]
	if e.Kind != events.KindJob || e.Bead != "mw-1" || e.Actor != "dispatch@laptop" || e.From != "scheduled" || e.To != "running" || !e.Ts.Equal(now) || e.Lane != events.LaneNormal {
		t.Fatalf("the log holds %+v, want the job event stamped %v in the normal lane", e, now)
	}
}

func TestEmitRefusesAnEventItsMachineForbidsAndWritesNothing(t *testing.T) {
	log := &apptest.FakeEventLog{}
	_, err := application.EventEmit{
		Log:   log,
		Now:   time.Now,
		Event: events.Event{Kind: events.KindJob, From: events.JobScheduled, To: events.JobDone},
	}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), `the job machine has no transition from "scheduled" to "done"`) {
		t.Fatalf("expected the transition refused by name, got %v", err)
	}
	if n := len(log.All()); n != 0 {
		t.Fatalf("a refused emit wrote %d events", n)
	}
}

func appendJobs(t *testing.T, log application.EventLog, n int) {
	t.Helper()
	ts := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	var batch []events.Event
	for i := 0; i < n; i++ {
		batch = append(batch, events.Event{Ts: ts, Kind: events.KindMail, Bead: "mw-m" + string(rune('1'+i)), Actor: "mw@laptop", Detail: "mayor", Lane: events.LaneNormal})
	}
	if _, err := log.Append(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
}

func TestTailPrintsTheEventsAfterSinceAsLines(t *testing.T) {
	log := &apptest.FakeEventLog{}
	appendJobs(t, log, 3)
	var out bytes.Buffer
	if err := (application.EventTail{Log: log, Since: 1, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "2 2026-10-01T13:00:00Z mail mw-m2 mw@laptop - mayor\n3 2026-10-01T13:00:00Z mail mw-m3 mw@laptop - mayor\n"
	if out.String() != want {
		t.Fatalf("tail printed\n%q\nwant\n%q", out.String(), want)
	}
}

func TestAnEventLineShowsItsTransition(t *testing.T) {
	ts := time.Date(2026, 10, 1, 13, 2, 7, 0, time.UTC)
	cases := []struct {
		e    events.Event
		want string
	}{
		{events.Event{Seq: 41, Ts: ts, Kind: events.KindBeadChanged, Bead: "mw-1", Actor: "mw@laptop", From: "open", To: "claimed", Detail: "status"}, "41 2026-10-01T13:02:07Z bead_changed mw-1 mw@laptop open->claimed status"},
		{events.Event{Seq: 42, Ts: ts, Kind: events.KindBeadChanged, Bead: "mw-1", Actor: "mw@laptop", To: "held", Detail: "status"}, "42 2026-10-01T13:02:07Z bead_changed mw-1 mw@laptop (start)->held status"},
		{events.Event{Seq: 43, Ts: ts, Kind: events.KindJob, Actor: "dispatch@laptop", From: "scheduled", To: "running"}, "43 2026-10-01T13:02:07Z job - dispatch@laptop scheduled->running"},
	}
	for _, c := range cases {
		if got := application.EventLine(c.e); got != c.want {
			t.Errorf("EventLine = %q, want %q", got, c.want)
		}
	}
}

func TestTailFollowPrintsWhatIsAppendedLater(t *testing.T) {
	log := &apptest.FakeEventLog{}
	appendJobs(t, log, 1)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var out bytes.Buffer
	sleeps := 0
	err := application.EventTail{
		Log: log, Follow: true, Out: &out,
		Sleep: func(context.Context, time.Duration) error {
			sleeps++
			if sleeps == 1 {
				appendJobs(t, log, 2)
				return nil
			}
			stop()
			return ctx.Err()
		},
	}.Run(ctx)
	if err != nil {
		t.Fatalf("expected a clean stop, got %v", err)
	}
	if got := strings.Count(out.String(), "\n"); got != 3 {
		t.Fatalf("tail --follow printed %d lines, want 3 (one before, two after):\n%s", got, out.String())
	}
	if !strings.HasPrefix(strings.Split(out.String(), "\n")[2], "3 ") {
		t.Fatalf("expected the third line to be seq 3, got:\n%s", out.String())
	}
}

func TestEventEmitEmergencyWritesTheEventInTheEmergencyLane(t *testing.T) {
	log := &apptest.FakeEventLog{}
	got, err := application.EventEmit{
		Log: log, Now: func() time.Time { return time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC) }, Emergency: true,
		Event: events.Event{Kind: events.KindJob, Actor: "doctor@laptop", From: events.JobRunning, To: events.JobFailed, Detail: "mayor-stale"},
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Lane != events.LaneEmergency {
		t.Fatalf("lane is %q, want emergency", got.Lane)
	}
	if in, _ := log.Since(context.Background(), 0); len(in) != 1 || in[0].Lane != events.LaneEmergency {
		t.Fatalf("the log holds %+v, want one emergency event", in)
	}
}

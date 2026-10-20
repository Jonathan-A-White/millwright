package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// plainLog hides the Newest of the log it wraps: a log that keeps nothing of
// its jobs' newest events.
type plainLog struct{ application.EventLog }

func TestNewestOfReadsTheWholeLogWhenTheLogKeepsNothingOfItsJobs(t *testing.T) {
	fake := &apptest.FakeEventLog{}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	evs := []events.Event{
		{Ts: at, Kind: events.KindJob, Actor: "dispatch@laptop", To: events.JobScheduled, Lane: events.LaneNormal},
		{Ts: at, Kind: events.KindJob, Actor: "dispatch@laptop", From: events.JobScheduled, To: events.JobRunning, Lane: events.LaneNormal},
		{Ts: at, Kind: events.KindJob, Actor: "dispatch@laptop", From: events.JobRunning, To: events.JobFailed, Detail: "boom", Lane: events.LaneNormal},
		{Ts: at, Kind: events.KindMail, Bead: "mw-m", Actor: "mw@laptop", Detail: "mayor", Lane: events.LaneNormal},
	}
	for i := 0; i < 3000; i++ {
		evs = append(evs, events.Event{Ts: at, Kind: events.KindMail, Bead: "mw-m", Actor: "mw@laptop", Detail: "mayor", Lane: events.LaneNormal})
	}
	if _, err := fake.Append(context.Background(), evs); err != nil {
		t.Fatal(err)
	}
	got, err := application.NewestOf(context.Background(), plainLog{fake})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := application.NewestOf(context.Background(), fake)
	if err != nil {
		t.Fatal(err)
	}
	job := got.Jobs["dispatch@laptop"]
	if got.Seq != 3004 || got.First.Seq != 1 || got.Other.Seq != 3004 || job.Last.To != events.JobFailed || job.Ended.Detail != "boom" || job.Running.Seq != 2 || job.Scheduled.Seq != 1 {
		t.Fatalf("newest = %+v; want the whole log folded", got)
	}
	if kept.Seq != got.Seq || kept.Jobs["dispatch@laptop"] != job {
		t.Fatalf("a log that keeps its newest events says %+v, a scan says %+v; want the same", kept, got)
	}
}

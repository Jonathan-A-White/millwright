package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// batchLandEvents are the events of a batch-land job at one host: each state in
// turn, with the detail the follower wrote.
func batchLandEvents(t *testing.T, log *apptest.FakeEventLog, steps ...events.Event) {
	t.Helper()
	for i := range steps {
		steps[i].Kind = events.KindJob
		steps[i].Lane = events.LaneNormal
		steps[i].Actor = application.BatchLandJobName + "@laptop"
	}
	if _, err := log.Append(context.Background(), steps); err != nil {
		t.Fatal(err)
	}
}

// A follower that restarts in the middle of a batch writes a scheduled event
// whose reason names no rig; the batch it runs is still the one for the rigs
// first scheduled (mw-gq6.727).
func TestBatchJobsRunningNamesTheRigsAfterTheFollowerRestarted(t *testing.T) {
	at := func(h, m, s int) time.Time { return time.Date(2026, 10, 23, h, m, s, 0, time.UTC) }
	cases := map[string][]events.Event{
		"restarted once": {
			{Ts: at(17, 11, 9), To: events.JobScheduled, Detail: "landing of lampas (6 queued)"},
			{Ts: at(17, 11, 10), From: events.JobScheduled, To: events.JobRunning},
			{Ts: at(17, 17, 50), From: events.JobRunning, To: events.JobDone, Detail: application.SpringInterruptedDetail},
			{Ts: at(17, 18, 0), From: events.JobDone, To: events.JobScheduled, Detail: application.SpringRestartedReason},
			{Ts: at(17, 18, 1), From: events.JobScheduled, To: events.JobRunning},
		},
		"restarted twice": {
			{Ts: at(17, 11, 9), To: events.JobScheduled, Detail: "landing of lampas (6 queued)"},
			{Ts: at(17, 11, 10), From: events.JobScheduled, To: events.JobRunning},
			{Ts: at(17, 17, 50), From: events.JobRunning, To: events.JobDone, Detail: application.SpringInterruptedDetail},
			{Ts: at(17, 18, 0), From: events.JobDone, To: events.JobScheduled, Detail: application.SpringRestartedReason},
			{Ts: at(17, 18, 1), From: events.JobScheduled, To: events.JobRunning},
			{Ts: at(17, 19, 50), From: events.JobRunning, To: events.JobDone, Detail: application.SpringInterruptedDetail},
			{Ts: at(17, 20, 0), From: events.JobDone, To: events.JobScheduled, Detail: application.SpringRestartedReason},
			{Ts: at(17, 20, 1), From: events.JobScheduled, To: events.JobRunning},
		},
	}
	for name, evs := range cases {
		t.Run(name, func(t *testing.T) {
			log := &apptest.FakeEventLog{}
			batchLandEvents(t, log, evs...)
			running := application.Status{Log: log}.BatchJobsRunning(context.Background())
			last := evs[len(evs)-1].Ts
			if got, ok := running["lampas"]; !ok || !got.Equal(last) {
				t.Fatalf("the rigs of the batch running = %v, want lampas since %v", running, last)
			}
		})
	}
}

// A restarted pass with no scheduled event before it that names a rig names none.
func TestBatchJobsRunningNamesNoRigForARestartWithNoBatchBefore(t *testing.T) {
	at := time.Date(2026, 10, 23, 17, 18, 0, 0, time.UTC)
	log := &apptest.FakeEventLog{}
	batchLandEvents(t, log,
		events.Event{Ts: at, To: events.JobScheduled, Detail: application.SpringRestartedReason},
		events.Event{Ts: at, From: events.JobScheduled, To: events.JobRunning},
	)
	if running := (application.Status{Log: log}).BatchJobsRunning(context.Background()); len(running) != 0 {
		t.Fatalf("rigs of the batch running = %v, want none", running)
	}
}

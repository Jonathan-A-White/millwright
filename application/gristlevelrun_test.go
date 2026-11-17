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

// mw-gq6.1387: the grist smoke of a level pass waits for the mill's answer
// inside the follower, as the sleep pass waits for its agent, but the pass was
// not marked as a model run, so the restart a landing of the factory rig asks
// for cut it. A pass of the grist-level job is marked while it waits, and the
// restart waits for it as it waits for the sleep pass.
func TestAGristLevelPassIsMarkedWhileItWaitsForTheMill(t *testing.T) {
	marks := &apptest.FakeAgentRunMarks{}
	log := &apptest.FakeEventLog{}
	entered := make(chan struct{}, 1)
	answer := make(chan struct{})
	job := application.GristLevelJob(time.Hour, func(ctx context.Context) error {
		entered <- struct{}{}
		select { // the grist smoke waiting for the mill's answer
		case <-answer:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	spring := &application.EventSpring{
		Log: log, Host: "laptop", Jobs: []application.SpringJob{job},
		Now: func() time.Time { return now }, Runs: marks,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := spring.Spring(ctx); err != nil { // takes its bearings
		t.Fatal(err)
	}
	if _, err := log.Append(ctx, []events.Event{{
		Ts: now, Kind: events.KindBeadChanged, Bead: "mw-a", Actor: "mw@laptop",
		From: events.BeadRunning, To: events.BeadLanded, Detail: "status", Lane: events.LaneNormal,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := spring.Spring(ctx); err != nil { // a bead landed
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the grist-level pass never began")
	}

	live, _, _ := marks.Open(ctx)
	if len(live) != 1 || live[0].Job != application.GristLevelJobName {
		t.Fatalf("the pass is not marked as a model run while it waits for the mill: %v", live)
	}

	// A restart under it waits, as it waits for the sleep pass.
	units := &apptest.FakeUnitRestarter{}
	restart := &application.RunGuardedRestarter{Units: units, Runs: marks, Poll: time.Millisecond, Limit: 10 * time.Second}
	var notes []string
	restarted := make(chan struct{})
	go func() {
		defer close(restarted)
		notes, _ = application.RestartFactoryUnits(context.Background(), restart, application.FactoryRig)
	}()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-restarted:
		t.Fatalf("the follower was restarted while the mill's answer was awaited: %v", units.Events)
	default:
	}
	if len(units.Asked) != 0 {
		t.Fatalf("units asked to restart under a run: %v", units.Asked)
	}

	close(answer)
	spring.Wait()
	<-restarted
	if len(units.Asked) != 1 || units.Asked[0] != application.FollowerUnit {
		t.Fatalf("the follower was not restarted after the pass: %v", units.Asked)
	}
	if said := strings.Join(notes, "\n"); !strings.Contains(said, "waited") || !strings.Contains(said, application.GristLevelJobName) {
		t.Errorf("the notes do not say the restart waited for the grist-level pass: %q", said)
	}
	if live, _, _ := marks.Open(ctx); len(live) != 0 {
		t.Errorf("a mark of the ended pass is left: %v", live)
	}
}

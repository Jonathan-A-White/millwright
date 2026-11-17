package application_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1389: a restart that waits for the follower's model runs to end did not
// keep a new one from beginning, so a run that began in the wait lengthened it,
// up to the limit, and was cut there.
func TestNoModelRunBeginsWhileARestartWaits(t *testing.T) {
	marks := &apptest.FakeAgentRunMarks{}
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	var began atomic.Int32
	job := application.SpringJob{
		Name: "todo-rank", Reason: application.ClockReason, Every: time.Minute, RunsAgent: true,
		Run: func(context.Context) error { began.Add(1); return nil },
	}
	spring := &application.EventSpring{
		Log: &apptest.FakeEventLog{}, Host: "laptop", Jobs: []application.SpringJob{job},
		Now: func() time.Time { return now }, Runs: marks,
	}
	ctx := context.Background()
	if err := spring.Spring(ctx); err != nil { // takes its bearings
		t.Fatal(err)
	}
	spring.Wait()
	first := began.Load() // a clock job is due at once, at the first look

	// A sleep pass is under way and a landing's restart waits for it.
	if err := marks.Begin(ctx, application.AgentRun{Job: "sleep", Host: "laptop", Pid: 1, Began: now}); err != nil {
		t.Fatal(err)
	}
	units := &apptest.FakeUnitRestarter{}
	waits := 0
	restart := &application.RunGuardedRestarter{
		Units: units, Runs: marks, Limit: time.Hour, Poll: time.Second,
		Now: func() time.Time { return now },
		Wait: func(ctx context.Context, d time.Duration) error {
			waits++
			now = now.Add(2 * time.Minute) // the todo job's clock is due in each wait
			if waits <= 2 {
				if err := spring.Spring(ctx); err != nil {
					t.Fatal(err)
				}
				spring.Wait()
				if got := began.Load(); got != first {
					t.Errorf("a model run began while the restart waited (wait %d): %d runs, %d before", waits, got, first)
				}
				return nil
			}
			return marks.End(ctx, application.AgentRun{Job: "sleep"}) // the sleep pass ends
		},
	}
	if _, err := restart.TryRestart(ctx, application.FollowerUnit); err != nil {
		t.Fatal(err)
	}
	if len(units.Asked) != 1 {
		t.Fatalf("the follower was not restarted once the run ended: %v", units.Asked)
	}

	// The restart is over: the held run begins at the next look.
	if err := spring.Spring(ctx); err != nil {
		t.Fatal(err)
	}
	spring.Wait()
	if got := began.Load(); got != first+1 {
		t.Fatalf("the held run did not begin once the restart was done: %d runs", got)
	}
}

// A hold has an end: a restarter that died leaves no run held for good.
func TestAHoldLapsesAtItsEnd(t *testing.T) {
	marks := &apptest.FakeAgentRunMarks{}
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	var began atomic.Int32
	job := application.SpringJob{
		Name: "todo-rank", Reason: application.ClockReason, Every: time.Minute, RunsAgent: true,
		Run: func(context.Context) error { began.Add(1); return nil },
	}
	spring := &application.EventSpring{
		Log: &apptest.FakeEventLog{}, Host: "laptop", Jobs: []application.SpringJob{job},
		Now: func() time.Time { return now }, Runs: marks,
	}
	ctx := context.Background()
	_ = spring.Spring(ctx)
	spring.Wait()
	first := began.Load()
	_ = marks.Hold(ctx, "restart", now.Add(10*time.Minute))
	now = now.Add(2 * time.Minute)
	_ = spring.Spring(ctx)
	spring.Wait()
	if began.Load() != first {
		t.Fatalf("a run began under a hold")
	}
	now = now.Add(20 * time.Minute)
	_ = spring.Spring(ctx)
	spring.Wait()
	if began.Load() != first+1 {
		t.Fatalf("the run did not begin after the hold lapsed: %d", began.Load())
	}
}

// The restart a self-update leaves to a later tick holds the model runs too: it
// is waiting for the follower, whatever process asked.
func TestARestartLeftToALaterTickHoldsTheModelRunsUntilItIsDone(t *testing.T) {
	update, units, marks, _, clock := aSelfUpdateUnderAModelRun(t)
	ctx := context.Background()
	update.Run(ctx)
	if !application.RunsHeld(ctx, marks, *clock) {
		t.Fatal("no model run is held back while the restart is left to a later tick")
	}
	*clock = clock.Add(15 * time.Minute)
	update.Run(ctx)
	if !application.RunsHeld(ctx, marks, *clock) {
		t.Fatal("the hold lapsed while the runs went on")
	}
	_ = marks.End(ctx, application.AgentRun{Job: "sleep"})
	update.Run(ctx)
	if len(units.Asked) != 1 {
		t.Fatalf("the follower was not restarted: %v", units.Asked)
	}
	if application.RunsHeld(ctx, marks, *clock) {
		t.Fatal("the hold is left after the restart")
	}
}

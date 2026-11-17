package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aSelfUpdateUnderAModelRun is the factory update of aFactoryUpdate on a host
// whose follower has a sleep run under way, with the guarded restarter a tick
// builds through hostUnits. The restarter's wait fails the test: a tick never
// waits for a run (mw-gq6.1388).
func aSelfUpdateUnderAModelRun(t *testing.T) (application.SelfUpdate, *apptest.FakeUnitRestarter, *apptest.FakeAgentRunMarks, fakeBuilt, *time.Time) {
	t.Helper()
	update, _, _, built := aFactoryUpdate()
	units := &apptest.FakeUnitRestarter{}
	marks := &apptest.FakeAgentRunMarks{}
	if err := marks.Begin(context.Background(), application.AgentRun{Job: "sleep", Host: "laptop", Pid: 4242, Began: time.Date(2026, 11, 15, 7, 30, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 15, 7, 40, 0, 0, time.UTC)
	update.Units = &application.RunGuardedRestarter{
		Units: units, Runs: marks,
		Now: func() time.Time { return clock },
		Wait: func(context.Context, time.Duration) error {
			t.Error("the tick waited for a model run: a tick must return at once")
			clock = clock.Add(application.AgentRunWaitLimit) // so that the wait ends and the test fails, not hangs
			return nil
		},
	}
	update.Now = func() time.Time { return clock }
	return update, units, marks, built, &clock
}

// mw-gq6.1388: the restart's wait (up to 30 minutes) ran inside the Millhand
// tick and the dispatch tick through SelfUpdate, and held a dispatch pass.
func TestASelfUpdateRestartThatWaitsForARunDoesNotHoldTheTick(t *testing.T) {
	update, units, marks, built, clock := aSelfUpdateUnderAModelRun(t)

	began := time.Now() //mw:realclock a test of how long the real call took
	notes := update.Run(context.Background())
	if time.Since(began) > 5*time.Second { //mw:realclock as above
		t.Fatalf("the tick was held for %s", time.Since(began))
	}

	said := strings.Join(notes, "\n")
	if !strings.Contains(said, "built") {
		t.Errorf("the tick did not say it built: %q", said)
	}
	if !strings.Contains(said, "mw-view-follow.service") || !strings.Contains(said, "a later tick") {
		t.Errorf("the tick does not say the follower's restart is left to a later tick: %q", said)
	}
	if len(units.Asked) != 0 {
		t.Fatalf("the follower was asked to restart under a run: %v", units.Asked)
	}

	// A later tick, the run still under way: the restart is still left.
	*clock = clock.Add(15 * time.Minute)
	later := strings.Join(update.Run(context.Background()), "\n")
	if len(units.Asked) != 0 {
		t.Fatalf("the follower was restarted under a run: %v", units.Asked)
	}
	if strings.Contains(later, "a later tick") {
		t.Errorf("the restart left to a later tick was said again: %q", later)
	}

	// The run ends; the next tick restarts the follower on the new build, once.
	if err := marks.End(context.Background(), application.AgentRun{Job: "sleep"}); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(15 * time.Minute)
	restarted := strings.Join(update.Run(context.Background()), "\n")
	if len(units.Asked) != 1 || units.Asked[0] != application.FollowerUnit {
		t.Fatalf("the follower was not restarted after the run ended: %v", units.Asked)
	}
	if !strings.Contains(restarted, "restarted "+application.FollowerUnit+" on the new build") {
		t.Errorf("the tick does not say the follower was restarted: %q", restarted)
	}
	update.Run(context.Background())
	if len(units.Asked) != 1 {
		t.Errorf("the follower was restarted twice for one build: %v", units.Asked)
	}
	if built["mw-view-follow.service.restart-pending"] != "" {
		t.Errorf("the pending restart is left marked: %v", built)
	}
}

// A run that does not end is cut at the limit, as a landing's restart cuts it,
// and the tick that does it says which run, without waiting.
func TestASelfUpdateRestartLeftToALaterTickGoesAheadAtTheLimitAndSaysTheCutRun(t *testing.T) {
	update, units, _, _, clock := aSelfUpdateUnderAModelRun(t)
	update.Run(context.Background())

	*clock = clock.Add(application.AgentRunWaitLimit + time.Minute)
	notes := strings.Join(update.Run(context.Background()), "\n")

	if len(units.Asked) != 1 || units.Asked[0] != application.FollowerUnit {
		t.Fatalf("the follower was not restarted at the limit: %v", units.Asked)
	}
	for _, want := range []string{"restarted " + application.FollowerUnit + " on the new build", "a run was cut", "sleep"} {
		if !strings.Contains(notes, want) {
			t.Errorf("the notes lack %q: %q", want, notes)
		}
	}
}

// With no model run under way the self-update restarts the follower at once, as
// it did.
func TestASelfUpdateWithNoRunUnderWayRestartsTheFollowerAtOnce(t *testing.T) {
	update, units, marks, _, _ := aSelfUpdateUnderAModelRun(t)
	if err := marks.End(context.Background(), application.AgentRun{Job: "sleep"}); err != nil {
		t.Fatal(err)
	}
	notes := strings.Join(update.Run(context.Background()), "\n")
	if len(units.Asked) != 1 || !strings.Contains(notes, "restarted "+application.FollowerUnit+" on the new build") {
		t.Fatalf("asked %v, notes %q", units.Asked, notes)
	}
}

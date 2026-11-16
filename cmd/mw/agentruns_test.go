package main

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// The restarts a landing and a build make go through the guard, so that none
// cuts a model run of the follower (mw-gq6.1339).
func TestTheHostRestartsUnitsThroughTheGuardThatWaitsForModelRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	guard, ok := hostUnits().(*application.RunGuardedRestarter)
	if !ok || guard.Runs == nil {
		t.Fatalf("hostUnits = %T, want the guarded restarter with marks", hostUnits())
	}
	if hostAgentRuns() == nil {
		t.Fatal("no marks for the follower's model runs")
	}
}

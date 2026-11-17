package rig_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// mw-gq6.1421: the bound of the run of the failing tests on main starts after
// the wait for a test-runner slot, not before it.

func TestARunOfNamedTestsIsCutAtItsBoundOnceItHasStarted(t *testing.T) {
	checks := rig.NewChecks(rig.WithNamedCommands(map[string]string{"lampas": "sleep 30"}))
	began := time.Now()
	_, err := checks.RunNamed(application.WithRunBound(context.Background(), 200*time.Millisecond), "lampas", t.TempDir(), []string{"a.test.ts"})
	var cut *application.RunCutError
	if !errors.As(err, &cut) {
		t.Fatalf("expected the run to be cut at its bound with a RunCutError, got %v", err)
	}
	if cut.Bound != 200*time.Millisecond {
		t.Errorf("expected the cut to name its bound, got %s", cut.Bound)
	}
	if took := time.Since(began); took > 20*time.Second {
		t.Errorf("expected the run stopped near its bound, it went on for %s", took)
	}
}

func TestAWaitForATestRunnerSlotIsNotCountedInTheBoundOfARun(t *testing.T) {
	dir := t.TempDir()
	holder, err := rig.NewTestRuns(dir, 1).Take(context.Background(), "a run going")
	if err != nil {
		t.Fatalf("taking the one slot: %v", err)
	}
	// The slot is given back after longer than the bound.
	time.AfterFunc(1500*time.Millisecond, holder)

	checks := rig.NewChecks(
		rig.WithTestRuns(rig.NewTestRuns(dir, 1, rig.WithTestRunsClock(time.Now, func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(20 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}))),
		rig.WithNamedCommands(map[string]string{"lampas": "true"}))
	run, err := checks.RunNamed(application.WithRunBound(context.Background(), time.Second), "lampas", t.TempDir(), []string{"a.test.ts"})
	if err != nil {
		t.Fatalf("expected a run that waited 1.5s for its slot to run inside a bound of 1s, got %v", err)
	}
	if !run.Ran || !run.Passed {
		t.Errorf("expected the run to have been made and passed, got %+v", run)
	}
}

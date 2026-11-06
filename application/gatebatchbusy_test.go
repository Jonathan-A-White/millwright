package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1007: a batch whose gate waited for the host to calm and found it busy
// but not far over its cores ran the rig's tests anyway, and a good batch was
// held for a flake of the load (batch 410 of lampas, at 30.4 of 20 cores). The
// batch waits for the next landing instead.

// aBatchGateAtLoad runs the gate of a batch on a host whose readings are the
// ones given, and returns what it printed and how the tests were asked.
func aBatchGateAtLoad(t *testing.T, readings ...application.LoadReading) (checks *scriptedChecks, checked application.Checked, err error, printed string) {
	t.Helper()
	checks = &scriptedChecks{passes: []bool{true}}
	var out bytes.Buffer
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: newFakeVault(), Checks: checks,
		Load:     &apptest.FakeHostLoad{Readings: readings},
		LoadWait: func(context.Context, time.Duration) error { return nil },
		// The wait is one step of the bound, so a busy host stays busy through it.
		LoadBound: time.Minute, LoadPoll: time.Minute,
		Host: "laptop", Seat: "builder", Out: &out, Err: &out,
	}}
	report := &application.BatchReport{Rig: "lampas", Number: 410, Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}}}
	checked, err, _ = land.GateTheBatchRun(context.Background(), t.TempDir(), report)
	return checks, checked, err, out.String()
}

func TestABatchWhoseWaitEndsWithTheHostBusyButNotFarOverRunsNoTestsAndSaysItWaitsForTheNextLanding(t *testing.T) {
	checks, _, err, printed := aBatchGateAtLoad(t, halfOver())

	if checks.runs != 0 {
		t.Errorf("expected no run of the rig's tests on a host still over its cores after the wait, got %d", checks.runs)
	}
	want := "host still at load 30.0 of 20: the batch waits for the next landing"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("expected the gate to end with %q, got %v", want, err)
	}
	if !strings.Contains(printed, want) {
		t.Errorf("expected the log to say %q, got:\n%s", want, printed)
	}
	if strings.Contains(printed, "ran anyway") {
		t.Errorf("expected nothing to run anyway, got:\n%s", printed)
	}
	if errors.As(err, new(application.HostBusyError)) {
		t.Errorf("expected a host not far over its cores not to be a HostBusyError, got %v", err)
	}
}

func TestABatchOnAHostUnderItsCoresRunsTheTestsAsBefore(t *testing.T) {
	checks, checked, err, _ := aBatchGateAtLoad(t, calm())

	if err != nil || checks.runs != 1 || !checked.Passed {
		t.Errorf("expected one passing run, got %d runs, %+v and %v", checks.runs, checked, err)
	}
}

func TestABatchWhoseHostCalmsDuringTheWaitRunsTheTests(t *testing.T) {
	checks, checked, err, _ := aBatchGateAtLoad(t, halfOver(), calm())

	if err != nil || checks.runs != 1 || !checked.Passed {
		t.Errorf("expected one passing run once the host calmed, got %d runs, %+v and %v", checks.runs, checked, err)
	}
}

func TestABatchWhoseWaitEndsFarOverTheCoresIsStillAHostBusyError(t *testing.T) {
	checks, _, err, _ := aBatchGateAtLoad(t, farOver())

	var busy application.HostBusyError
	if checks.runs != 0 || !errors.As(err, &busy) {
		t.Errorf("expected no run and a HostBusyError far over the cores, got %d runs and %v", checks.runs, err)
	}
}

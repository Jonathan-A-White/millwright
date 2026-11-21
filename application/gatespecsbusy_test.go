package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1079: the changed specs a batch's gate runs after its tests (gateWithSpecs)
// are dealt with as a gate's failure is, as specsRefusal does (mw-gq6.1076): a
// failure read with the host far over its cores waits for it to calm and runs the
// specs once more, the second run standing.

// aBatchSpecsGate runs the gate of a batch, tests and changed specs, on host.
func aBatchSpecsGate(t *testing.T, host *aSpecsHost) (checked application.Checked, err error, notes []string) {
	t.Helper()
	land := application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: newFakeVault(),
		Checks: gatePasses{}, SpecChecks: host, Load: host,
		LoadWait: func(ctx context.Context, d time.Duration) error { return host.wait(ctx, d) },
		Host:     "laptop", Seat: "builder", Out: io.Discard, Err: io.Discard,
	}}
	report := &application.BatchReport{Rig: "lampas", Number: 410, Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}}}
	return land.GateTheBatchRun(context.Background(), t.TempDir(), report)
}

func TestABatchsChangedSpecsThatFailFarOverPassOnTheCalmRunAndSaySoTwice(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: farOverAfterTheFirstRun(3, false)}
	checked, err, notes := aBatchSpecsGate(t, host)
	if err != nil || !checked.Passed {
		t.Fatalf("expected the batch's gate to pass on the calm run, got %+v and %v", checked, err)
	}
	if host.runs != 2 {
		t.Errorf("expected the changed specs run twice, got %d runs", host.runs)
	}
	said := strings.Join(notes, "\n")
	for _, want := range []string{"the changed specs ran twice", "load 24.0 of 8 cores", "Flaked: TestA"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected a note saying %q, got:\n%s", want, said)
		}
	}
}

func TestABatchsChangedSpecsThatFailFarOverAndAgainOnTheCalmRunFailOnTheSecondRun(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), failedSpecs("TestC")}, loads: farOverAfterTheFirstRun(2, false)}
	checked, err, _ := aBatchSpecsGate(t, host)
	if err != nil || checked.Passed || checked.NotRun {
		t.Fatalf("expected a failed verdict, got %+v and %v", checked, err)
	}
	if host.runs != 2 {
		t.Errorf("expected the changed specs run twice, got %d runs", host.runs)
	}
	if !strings.Contains(checked.Output, "TestC") || strings.Contains(checked.Output, "TestA") {
		t.Errorf("expected the second run's failure, TestC, and not the first's, got:\n%s", checked.Output)
	}
}

func TestABatchsChangedSpecsThatFailOnACalmOrOnlyABusyHostAreRunOnceAndFail(t *testing.T) {
	for name, load := range map[string]application.LoadReading{"calm": specsCalm, "busy": specsBusy} {
		t.Run(name, func(t *testing.T) {
			host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: func(runs, _ int) application.LoadReading {
				if runs == 0 {
					return specsCalm // the tests before the specs
				}
				return load
			}}
			checked, err, _ := aBatchSpecsGate(t, host)
			if err != nil || checked.Passed || checked.NotRun {
				t.Fatalf("expected a failed verdict, got %+v and %v", checked, err)
			}
			if host.runs != 1 {
				t.Errorf("expected one run of the changed specs, got %d", host.runs)
			}
		})
	}
}

func TestABatchsChangedSpecsThatFailFarOverOnAHostThatNeverCalmsAreNotRunAndNameTheLoad(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: farOverAfterTheFirstRun(1<<30, false)}
	checked, err, _ := aBatchSpecsGate(t, host)
	if err == nil || checked.Passed {
		t.Fatalf("expected a gate that did not run, not a failed verdict, got %+v and %v", checked, err)
	}
	if !strings.Contains(err.Error(), "24.0 of 8") {
		t.Errorf("expected the error to name the host's load, got %v", err)
	}
	if host.runs != 1 {
		t.Errorf("expected one run of the changed specs, got %d", host.runs)
	}
}

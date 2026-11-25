package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1080: the process the detached mw check --specs starts runs the changed
// specs as the foreground close-out does (specsRefusal, mw-gq6.1076): a failure
// read with the host far over its cores waits for the host to calm and is run once
// more, the second run standing; a host that never calms is not-run, not failed.

type keptSpecs struct {
	Result string   `json:"result"`
	Output string   `json:"output"`
	Notes  []string `json:"notes"`
}

// theDetachedSpecsWorker runs the specs worker of a whole check against host, and
// returns what it kept as the specs' record.
func theDetachedSpecsWorker(t *testing.T, host *aSpecsHost) keptSpecs {
	t.Helper()
	w := newWholeCheckBesideTheGateLock(t, &slowChecks{pass: true})
	records := &logRecords{dir: t.TempDir()}
	k := application.Check{
		Tracker: aStoryOnTheLaptop(), Landing: aLandingAtATip{&fakeRetryLanding{AheadCount: 1}, "abc1234"},
		Checks: &slowChecks{pass: true}, SpecChecks: host, Records: records, Logs: records,
		Load: host, LoadWait: host.wait, SpecsWorker: true, Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": w.rigDir}, Out: writerOf(&lockedBuffer{}),
	}
	if _, err := k.Run(context.Background(), "mw-x.1"); err != nil {
		t.Logf("the worker ended: %v", err)
	}
	var kept keptSpecs
	text, rerr := records.ReadRunFile(context.Background(), "mw-x.1", application.CheckSpecsResultFileName(1))
	if rerr != nil {
		t.Fatalf("expected the specs' record to be kept: %v", rerr)
	}
	if err := json.Unmarshal([]byte(text), &kept); err != nil {
		t.Fatalf("expected the specs' record to be kept, got %q (%v)", text, err)
	}
	return kept
}

func TestDetachedSpecsThatFailFarOverPassOnTheCalmRunAndAreRecordedPassed(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: farOverAfterTheFirstRun(3, false)}
	kept := theDetachedSpecsWorker(t, host)
	if kept.Result != "passed" || host.runs != 2 {
		t.Fatalf("expected passed after two runs, got %q after %d", kept.Result, host.runs)
	}
	notes := strings.Join(kept.Notes, "\n")
	for _, want := range []string{"the changed specs ran twice", "load 24.0 of 8 cores", "Flaked: TestA"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected a note saying %q, got:\n%s", want, notes)
		}
	}
}

func TestDetachedSpecsThatFailFarOverAndAgainAreRecordedFailedFromTheSecondRun(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), failedSpecs("TestC")}, loads: farOverAfterTheFirstRun(2, false)}
	kept := theDetachedSpecsWorker(t, host)
	if kept.Result != "failed" || host.runs != 2 {
		t.Fatalf("expected failed after two runs, got %q after %d", kept.Result, host.runs)
	}
	if !strings.Contains(kept.Output, "TestC") || strings.Contains(kept.Output, "TestA") {
		t.Errorf("expected the second run's output, got %q", kept.Output)
	}
}

func TestDetachedSpecsThatFailOnACalmHostAreRunOnceAndRecordedFailed(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: func(int, int) application.LoadReading { return specsCalm }}
	kept := theDetachedSpecsWorker(t, host)
	if kept.Result != "failed" || host.runs != 1 {
		t.Fatalf("expected failed after one run, got %q after %d", kept.Result, host.runs)
	}
}

func TestDetachedSpecsThatFailFarOverOnAHostThatNeverCalmsAreRecordedNotRun(t *testing.T) {
	host := &aSpecsHost{results: []application.SpecsChecked{failedSpecs("TestA"), passedSpecs()}, loads: farOverAfterTheFirstRun(1<<30, false)}
	kept := theDetachedSpecsWorker(t, host)
	if kept.Result != "notrun" || host.runs != 1 {
		t.Fatalf("expected notrun after one run, got %q after %d", kept.Result, host.runs)
	}
}

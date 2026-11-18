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

// mw-gq6.1506: a Builder whose gate check waited out its limit behind another
// story's gate ran nothing, left its steps open as it was told to, and the
// close-out refused it as open-steps and counted the attempt.

// aGateWaitNote is what a check that reached its wait limit leaves in the run.
const aGateWaitNote = "gave-up 16:36:01Z\nholder mw-5r3p30.1189\nwhy lampas's gate lock is still held by mw-5r3p30.1189, since 15:21:44Z\n"

// theCloseOutReadingTheRun is mw next on mw-gq6.9 with the records of its run.
func theCloseOutReadingTheRun(t *testing.T, tracker *apptest.FakeTracker, lands *aCommittedLanding, records map[string]string) (application.NextReport, error) {
	t.Helper()
	vault := newFakeVault()
	vault.written["mw-gq6.9/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	for name, text := range records {
		vault.written["mw-gq6.9/"+name] = text
	}
	return application.Next{
		Tracker: tracker, Vault: vault, GateRecords: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		Seat: "builder", Host: "vps",
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Rigs:      map[string]string{"millwright": "/rigs/millwright"},
		Out:       io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-gq6.9")
}

func TestAStoryWhoseGateCheckReachedItsWaitLimitIsRefusedAsAGateWaitNotOpenSteps(t *testing.T) {
	tracker, lands, _ := aStoryWhoseStepCloseLostTheLock(t, false)

	report, _ := theCloseOutReadingTheRun(t, tracker, lands, map[string]string{"check-1-limit.slot": aGateWaitNote})

	if report.Landed || report.Reason != application.ReasonGateWait {
		t.Fatalf("expected a refusal for gate-wait, got %s: %+v", report.Reason, report)
	}
	for _, want := range []string{"mw-5r3p30.1189", "not a failure"} {
		if !strings.Contains(report.Why, want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, report.Why)
		}
	}
	if strings.Contains(report.Why, "still open, so the formula was not finished") {
		t.Errorf("expected the refusal not to read as steps left open, got:\n%s", report.Why)
	}

	// Left to retry: open and unclaimed, and the attempt is not the story's own.
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.9")
	mustDo(t, err)
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		t.Errorf("expected the story open and unclaimed, got status %q held by %q", detail.Status, detail.Assignee)
	}
	if own, _ := application.OwnAttempts(detail); own != 0 {
		t.Errorf("expected the attempt not counted against the story, %d counted (causes %v)", own, detail.AttemptCauses)
	}
	if !application.ContinuesFrom(application.ReasonGateWait) {
		t.Errorf("expected the next attempt to continue the branch")
	}
}

func TestAStoryWhoseStepsTheBuilderLeftOpenIsStillRefusedAsOpenStepsAndCounted(t *testing.T) {
	tracker, lands, _ := aStoryWhoseStepCloseLostTheLock(t, false)

	report, _ := theCloseOutReadingTheRun(t, tracker, lands, nil)

	if report.Landed || report.Reason != application.ReasonOpenSteps {
		t.Fatalf("expected a refusal for %s, got %s: %+v", application.ReasonOpenSteps, report.Reason, report)
	}
	detail, err := tracker.ShowStory(context.Background(), "mw-gq6.9")
	mustDo(t, err)
	if own, _ := application.OwnAttempts(detail); own != 1 {
		t.Errorf("expected the attempt counted against the story, %d counted", own)
	}
}

func TestAWholeCheckThatReachesItsWaitLimitLeavesTheRecordTheCloseOutReadsAndALaterCallClearsIt(t *testing.T) {
	checks := &scriptedChecks{passes: []bool{true}}
	w := newWholeCheckBesideTheGateLock(t, checks)
	w.handBackAtOnce = true
	closeOut, err := w.gate.Take(context.Background(), "mw-5r3p30.1189", nil)
	mustDo(t, err)

	if _, err := w.check(100 * time.Millisecond); err == nil {
		t.Fatal("expected the check not to pass")
	}
	name := "mw-x.1/" + application.CheckLimitFileName(1)
	text, err := w.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckLimitFileName(1))
	if err != nil || !strings.Contains(text, "mw-5r3p30.1189") {
		t.Fatalf("expected %s to name the holder, got %q (%v)", name, text, err)
	}

	// The gate is free: the next call runs the tests, and what it ran is the newer word.
	closeOut.Holding.Release(context.Background())
	if _, err := w.check(2 * time.Second); err != nil {
		t.Fatalf("expected the check to pass once the gate was free, got %v", err)
	}
	if text, _ := w.records.ReadRunFile(context.Background(), "mw-x.1", application.CheckLimitFileName(1)); strings.TrimSpace(text) != "" {
		t.Errorf("expected the record taken away by the call that ran the tests, got %q", text)
	}
}

func TestACloseOutDoesNotReadAClearedWaitRecordAsAGateWait(t *testing.T) {
	tracker, lands, _ := aStoryWhoseStepCloseLostTheLock(t, false)

	report, _ := theCloseOutReadingTheRun(t, tracker, lands, map[string]string{"check-1-limit.slot": ""})

	if report.Reason != application.ReasonOpenSteps {
		t.Fatalf("expected a refusal for %s, got %s", application.ReasonOpenSteps, report.Reason)
	}
}

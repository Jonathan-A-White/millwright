package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1120: mw retry closed a story's formula with one bd call per bead, as
// mw next did before mw-gq6.990, so a close that met the Dolt server's i/o
// timeout left the formula to mw tidy. These tests retry a story whose formula
// closes fail the way bd's do (formulaTimeout and formulaUnsure are in
// next_formulaclosewrite_test.go).

type retriedFormula struct {
	report  application.RetryReport
	err     error
	tracker *formulaCloseFaults
	mail    *apptest.FakeMailbox
	waits   []time.Duration
}

// aRetriedStoryWhoseFormulaCloseFaults retries a story whose formula, two steps
// and a root, is closed over closes that fail the first fails times with err.
func aRetriedStoryWhoseFormulaCloseFaults(t *testing.T, fails int, err error, landed bool) *retriedFormula {
	t.Helper()
	ctx := context.Background()
	tracker := &formulaCloseFaults{FakeTracker: apptest.NewFakeTracker(), fails: fails, err: err, landed: landed}
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "Understand {{story}}"}, application.FormulaStep{Title: "Commit"})
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "vps"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story to retry"})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	poured, perr := tracker.PourFormula(ctx, "tdd-feature", "mw-x.1", "A story to retry", false)
	mustDo(t, perr)
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-x.1", map[string]string{application.MoleculeField: poured.RootID, application.AttemptsField: "1"}))
	run := &retriedFormula{tracker: tracker, mail: apptest.NewFakeMailbox()}
	retry := aRetry(tracker.FakeTracker, &fakeRetryLanding{}, &apptest.FakeVaultFiles{}, &strings.Builder{})
	retry.Tracker = tracker
	retry.Mailbox = run.mail
	retry.PushWait = 20 * time.Second
	retry.RetryWait = func(_ context.Context, d time.Duration) error {
		run.waits = append(run.waits, d)
		return nil
	}
	run.report, run.err = retry.Run(ctx, "mw-x.1")
	return run
}

func (r *retriedFormula) lostWriteMails(t *testing.T) []application.Message {
	t.Helper()
	inbox, _ := r.mail.Inbox(context.Background(), application.MayorMailbox)
	var lost []application.Message
	for _, m := range inbox {
		if strings.HasPrefix(m.Subject, "lost write") {
			lost = append(lost, m)
		}
	}
	return lost
}

func (r *retriedFormula) formulaIsShut(t *testing.T) bool {
	t.Helper()
	open, err := r.tracker.OpenMolecule(context.Background(), "f-mol-1")
	mustDo(t, err)
	return open.RootID == "" && len(open.Steps) == 0
}

func TestARetriedFormulaCloseThatMeetsAPassingFaultIsTriedAgainAndLeavesNothingForTidy(t *testing.T) {
	// The first close, of a step, and then the root's, fail; the others go through.
	for _, fails := range []int{1, 2} {
		run := aRetriedStoryWhoseFormulaCloseFaults(t, fails, formulaTimeout(), false)
		if run.err != nil {
			t.Fatalf("fails=%d: expected the retry to succeed, got %v", fails, run.err)
		}
		if !run.formulaIsShut(t) {
			t.Errorf("fails=%d: expected the formula closed, root and steps", fails)
		}
		if run.report.FormulaLeft != "" {
			t.Errorf("fails=%d: expected nothing left to mw tidy, got %q", fails, run.report.FormulaLeft)
		}
		if mails := run.lostWriteMails(t); len(mails) != 0 {
			t.Errorf("fails=%d: expected no lost-write mail, got %+v", fails, mails)
		}
		if len(run.waits) != fails {
			t.Errorf("fails=%d: expected a wait before each try again, got %v", fails, run.waits)
		}
	}
}

func TestAnIndeterminateRetriedFormulaCloseThatLandedIsNotWrittenAgain(t *testing.T) {
	run := aRetriedStoryWhoseFormulaCloseFaults(t, 1, errors.New(formulaUnsure), true)
	if run.err != nil || !run.formulaIsShut(t) || run.report.FormulaLeft != "" {
		t.Fatalf("expected the formula closed, got err %v and %+v", run.err, run.report)
	}
	if len(run.tracker.asked) != 3 {
		t.Errorf("expected the step that landed not closed a second time (two steps and the root asked once each), got %v", run.tracker.asked)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
}

func TestAnIndeterminateRetriedFormulaCloseThatDidNotLandIsTriedAgain(t *testing.T) {
	run := aRetriedStoryWhoseFormulaCloseFaults(t, 1, errors.New(formulaUnsure), false)
	if run.err != nil || !run.formulaIsShut(t) || run.report.FormulaLeft != "" {
		t.Fatalf("expected the formula closed, got err %v and %+v", run.err, run.report)
	}
	if len(run.tracker.asked) != 4 {
		t.Errorf("expected the failed close asked twice, then the other step and the root, got %v", run.tracker.asked)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
}

func TestARetriedFormulaCloseThatFailsEveryTryIsMailedOnceWithHowManyTimesItWasTried(t *testing.T) {
	run := aRetriedStoryWhoseFormulaCloseFaults(t, 1000, formulaTimeout(), false)
	if run.err != nil {
		t.Fatalf("expected the retry to go on, got %v", run.err)
	}
	if run.formulaIsShut(t) || run.report.FormulaLeft == "" {
		t.Fatalf("expected the formula left open and said so, got %+v", run.report)
	}
	mails := run.lostWriteMails(t)
	if len(mails) != 1 {
		t.Fatalf("expected one lost-write mail, got %+v", mails)
	}
	if !strings.Contains(mails[0].Body, "tried 5 times") || !strings.Contains(mails[0].Body, "i/o timeout") || !strings.Contains(mails[0].Body, "mw retry") {
		t.Errorf("expected the mail to name mw retry, say it was tried 5 times and quote bd, got:\n%s", mails[0].Body)
	}
	if len(run.tracker.asked) != application.ReadRetries+1 {
		t.Errorf("expected the first close asked %d times and no further, got %v", application.ReadRetries+1, run.tracker.asked)
	}
}

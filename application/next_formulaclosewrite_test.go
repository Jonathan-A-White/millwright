package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.990: mw next closed a story's formula with one bd call per step and
// one for the root, so a close that met the Dolt server's i/o timeout, or whose
// commit bd said was indeterminate, was lost and mailed to the Mayor as lost
// "after being tried again". These tests close a landed story's formula through
// a tracker whose closes of the formula's beads fail the way bd's do.

// formulaTimeout is the failure the gateway makes of bd's i/o timeout: a
// TransientFault.
func formulaTimeout() error {
	return &application.TransientFault{Why: "dolt server unreachable", Err: errors.New("bd close f-mol-1.1 --reason x: exit status 1: Error: failed to open database: Dolt server unreachable at 10.88.0.2:3307: dial tcp 10.88.0.2:3307: i/o timeout")}
}

const (
	formulaUnsure = "bd close f-mol-1.1: [mysql] packets.go:58 read tcp 10.88.0.3:50110->10.88.0.2:3307: i/o timeout\nError closing f-mol-1.1: write commit result indeterminate after connection loss (not retried to avoid double-apply)"
)

// formulaCloseFaults is a tracker whose closes of the beads of the formula (any
// id but the story's) fail the first fails times with err; with landed, the
// close that failed was made all the same, as an indeterminate one may have
// been.
type formulaCloseFaults struct {
	*apptest.FakeTracker
	skip   int // closes that go through before the first fails
	fails  int
	err    error
	landed bool
	asked  []string
}

func (f *formulaCloseFaults) CloseStory(ctx context.Context, id, reason string) error {
	if id == "mw-x.1" {
		return f.FakeTracker.CloseStory(ctx, id, reason)
	}
	f.asked = append(f.asked, id)
	if n := len(f.asked) - f.skip; n >= 1 && n <= f.fails {
		if f.landed {
			if err := f.FakeTracker.CloseStory(ctx, id, reason); err != nil {
				return err
			}
		}
		return f.err
	}
	return f.FakeTracker.CloseStory(ctx, id, reason)
}

type formulaClosed struct {
	report  application.NextReport
	err     error
	tracker *formulaCloseFaults
	mail    *apptest.FakeMailbox
	waits   []time.Duration
}

// aLandedStoryWhoseFormulaCloseFaults closes out a landed story whose formula,
// two steps and a root, is closed over closes that fail the first fails times
// with err.
func aLandedStoryWhoseFormulaCloseFaults(t *testing.T, fails int, err error, landed bool) *formulaClosed {
	t.Helper()
	return aLandedStoryWhoseFormulaCloseFaultsAfter(t, 0, fails, err, landed)
}

// aLandedStoryWhoseFormulaCloseFaultsAfter lets the first skip closes through.
func aLandedStoryWhoseFormulaCloseFaultsAfter(t *testing.T, skip, fails int, err error, landed bool) *formulaClosed {
	t.Helper()
	ctx := context.Background()
	tracker := &formulaCloseFaults{FakeTracker: apptest.NewFakeTracker(), skip: skip, fails: fails, err: err, landed: landed}
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "Understand {{story}}"}, application.FormulaStep{Title: "Commit"})
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story"})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	poured, perr := tracker.PourFormula(ctx, "tdd-feature", "mw-x.1", "A landed story", false)
	mustDo(t, perr)
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	lands := &fakeRetryLanding{}
	run := &formulaClosed{tracker: tracker, mail: apptest.NewFakeMailbox()}
	run.report, run.err = application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: run.mail,
		PushWait: 20 * time.Second,
		RetryWait: func(_ context.Context, d time.Duration) error {
			run.waits = append(run.waits, d)
			return nil
		},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	return run
}

func (r *formulaClosed) lostWriteMails(t *testing.T) []application.Message {
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

func (r *formulaClosed) formulaIsShut(t *testing.T) bool {
	t.Helper()
	open, err := r.tracker.OpenMolecule(context.Background(), "f-mol-1")
	mustDo(t, err)
	return open.RootID == "" && len(open.Steps) == 0
}

func TestAFormulaCloseThatMeetsAPassingFaultIsTriedAgainAndLandsWithNoLostWriteMail(t *testing.T) {
	// The first close, of a step, and the third, of the root, fail; the others go through.
	for _, fails := range []int{1, 2} {
		run := aLandedStoryWhoseFormulaCloseFaults(t, fails, formulaTimeout(), false)
		if run.err != nil || !run.report.Closed {
			t.Fatalf("fails=%d: expected the story landed and closed, got err %v and %+v", fails, run.err, run.report)
		}
		if !run.formulaIsShut(t) {
			t.Errorf("fails=%d: expected the formula closed, root and steps", fails)
		}
		if mails := run.lostWriteMails(t); len(mails) != 0 {
			t.Errorf("fails=%d: expected no lost-write mail, got %+v", fails, mails)
		}
		if len(run.waits) != fails {
			t.Errorf("fails=%d: expected a wait before each try again, got %v", fails, run.waits)
		}
	}
}

func TestAFormulaCloseThatMeetsAFaultOnTheRootIsTriedAgain(t *testing.T) {
	// With two steps the third close is the root's.
	run := aLandedStoryWhoseFormulaCloseFaultsAfter(t, 2, 2, formulaTimeout(), false)
	if !run.formulaIsShut(t) {
		t.Fatalf("expected the formula closed, got %+v", run.report)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
}

func TestAFormulaCloseThatFailsEveryTryStillMailsALostWriteAndSaysHowManyTimesItWasTried(t *testing.T) {
	run := aLandedStoryWhoseFormulaCloseFaults(t, 1000, formulaTimeout(), false)
	if run.formulaIsShut(t) {
		t.Fatalf("expected the formula still open")
	}
	mails := run.lostWriteMails(t)
	if len(mails) != 1 {
		t.Fatalf("expected one lost-write mail, got %+v", mails)
	}
	want := "tried " + "5" + " times"
	if !strings.Contains(mails[0].Body, want) || !strings.Contains(mails[0].Body, "i/o timeout") {
		t.Errorf("expected the mail to say it was %q and quote bd, got:\n%s", want, mails[0].Body)
	}
	if len(run.tracker.asked) != application.ReadRetries+1 {
		t.Errorf("expected the first close asked %d times and no further, got %v", application.ReadRetries+1, run.tracker.asked)
	}
}

func TestAnIndeterminateFormulaCloseThatLandedIsNotWrittenAgainAndMailsNothing(t *testing.T) {
	run := aLandedStoryWhoseFormulaCloseFaults(t, 1, errors.New(formulaUnsure), true)
	if !run.formulaIsShut(t) {
		t.Fatalf("expected the formula closed, got %+v", run.report)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
	if len(run.tracker.asked) != 3 {
		t.Errorf("expected the step that landed not closed a second time (two steps and the root asked once each), got %v", run.tracker.asked)
	}
}

func TestAnIndeterminateFormulaCloseThatDidNotLandIsTriedAgainAndMailsNothing(t *testing.T) {
	run := aLandedStoryWhoseFormulaCloseFaults(t, 1, errors.New(formulaUnsure), false)
	if !run.formulaIsShut(t) {
		t.Fatalf("expected the formula closed, got %+v", run.report)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
	if len(run.tracker.asked) != 4 {
		t.Errorf("expected the failed close asked twice, then the other step and the root, got %v", run.tracker.asked)
	}
}

func TestARefusedFormulaCloseThatIsNoPassingFaultIsNotTriedAgainAndIsMailedAsLostOnce(t *testing.T) {
	run := aLandedStoryWhoseFormulaCloseFaults(t, 1000, errors.New("bd close: exit status 1: Error: no issue found matching f-mol-1.1"), false)
	if len(run.tracker.asked) != 1 {
		t.Errorf("expected one close asked, got %v", run.tracker.asked)
	}
	mails := run.lostWriteMails(t)
	if len(mails) != 1 || !strings.Contains(mails[0].Body, "tried once") || strings.Contains(mails[0].Body, "times") {
		t.Fatalf("expected one lost-write mail that does not claim a retry, got %+v", mails)
	}
}

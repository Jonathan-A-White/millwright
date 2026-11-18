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
)

// mw-gq6.1505: a Builder's own `mw bd close` of a formula step that waited out
// its limit for the bd lock ran no bd, so the step stayed open, and the
// close-out refused the story as open-steps, spending an attempt on a step the
// Builder had closed as far as it could.

// closesThatFailOn is a tracker whose close of any bead fails with err, and
// counts the tries.
type closesThatFailOn struct {
	*apptest.FakeTracker
	err   error
	tries int
}

func (c *closesThatFailOn) CloseStory(ctx context.Context, id, reason string) error {
	c.tries++
	if c.err != nil {
		return c.err
	}
	return c.FakeTracker.CloseStory(ctx, id, reason)
}

// theMayorLandsOn is theMayorLands over any tracker.
func theMayorLandsOn(t *testing.T, tracker application.WorkTracker, lands application.Landing, locks application.BdLockNotes) (application.NextReport, error) {
	t.Helper()
	vault := newFakeVault()
	vault.written["mw-gq6.9/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	next := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands.(application.Worktrees), Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{},
		Seat: "builder", Host: "vps", BdLocks: locks,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Rigs:      map[string]string{"millwright": "/rigs/millwright"},
		Out:       io.Discard, Err: io.Discard,
	}
	return next.Run(context.Background(), "mw-gq6.9")
}

// aStoryWhoseStepCloseLostTheLock is a story with four open steps, the first of
// which the Builder's mw bd gave up closing for the bd lock.
func aStoryWhoseStepCloseLostTheLock(t *testing.T, notes bool) (*apptest.FakeTracker, *aCommittedLanding, *apptest.FakeBdLockNotes) {
	t.Helper()
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)
	molecule, _ := tracker.Poured("mw-gq6.9")
	locks := &apptest.FakeBdLockNotes{}
	if notes {
		mustDo(t, locks.Lose(context.Background(), []string{molecule.Steps[0].ID}, "bd (pid 4091466: bd list)"))
	}
	return tracker, lands, locks
}

func TestAStepCloseThatCouldNotGetTheBdLockIsRefusedAsThatNotAsOpenSteps(t *testing.T) {
	tracker, lands, locks := aStoryWhoseStepCloseLostTheLock(t, true)
	closes := &closesThatFailOn{FakeTracker: tracker, err: errors.New("waited 2m0s for the host to be quiet and bd was not run")}

	report, _ := theMayorLandsOn(t, closes, lands, locks)

	if report.Landed || report.Reason != application.ReasonStepCloseLock {
		t.Fatalf("expected a refusal for %s, got %s: %+v", application.ReasonStepCloseLock, report.Reason, report)
	}
	for _, want := range []string{"could not get the bd lock", "pid 4091466"} {
		if !strings.Contains(report.Why, want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, report.Why)
		}
	}
	if strings.Contains(report.Why, "still open, so the formula was not finished") {
		t.Errorf("expected the refusal not to read as steps left open, got:\n%s", report.Why)
	}
	// Handed back for its close-out alone: no attempt is spent.
	if !report.HandedBack {
		t.Errorf("expected the story handed back for its close-out alone, got %+v", report)
	}
}

func TestTheCloseOutsWaitForTheBdLockIsBounded(t *testing.T) {
	tracker, lands, locks := aStoryWhoseStepCloseLostTheLock(t, true)
	closes := &closesThatFailOn{FakeTracker: tracker, err: errors.New("waited 2m0s for the host to be quiet and bd was not run")}

	report, _ := theMayorLandsOn(t, closes, lands, locks)

	if closes.tries != application.StepCloseLockTries {
		t.Errorf("expected the close tried %d times and then given up, got %d tries", application.StepCloseLockTries, closes.tries)
	}
	if !strings.Contains(report.Why, "tries") {
		t.Errorf("expected the refusal to say it tried and gave up, got:\n%s", report.Why)
	}
}

func TestAStepCloseThatGetsTheBdLockOnTheCloseOutsTryIsClosedAndLands(t *testing.T) {
	tracker, lands, locks := aStoryWhoseStepCloseLostTheLock(t, true)

	// The three other steps the Builder closed itself.
	molecule, _ := tracker.Poured("mw-gq6.9")
	for _, step := range molecule.Steps[1:] {
		mustDo(t, tracker.CloseStory(context.Background(), step.ID, "done"))
	}
	report, err := theMayorLandsOn(t, tracker, lands, locks)

	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed once the step was closed, got err %v and %+v", err, report)
	}
	if open := openStepsOf(t, tracker); open != 0 {
		t.Errorf("expected no step left open, got %d", open)
	}
	if lost, _ := locks.Lost(context.Background(), []string{molecule.Steps[0].ID}); len(lost) != 0 {
		t.Errorf("expected the note of the lost close taken away, got %v", lost)
	}
}

func TestStepsTheBuilderLeftOpenAreStillRefusedAsOpenSteps(t *testing.T) {
	// No mw bd gave up on the lock for any step: they are the Builder's to close.
	tracker, lands, locks := aStoryWhoseStepCloseLostTheLock(t, false)
	report, _ := theMayorLandsOn(t, tracker, lands, locks)

	if report.Landed || report.Reason != application.ReasonOpenSteps {
		t.Fatalf("expected a refusal for %s, got %s: %+v", application.ReasonOpenSteps, report.Reason, report)
	}
}

func TestNotesOfLostClosesThatCannotBeReadLeaveTheStepsReadAsOpen(t *testing.T) {
	tracker, lands, locks := aStoryWhoseStepCloseLostTheLock(t, true)
	locks.ReadErr = errors.New("the state directory is unreadable")

	report, _ := theMayorLandsOn(t, tracker, lands, locks)

	if report.Reason != application.ReasonOpenSteps {
		t.Fatalf("expected a refusal for %s, got %s", application.ReasonOpenSteps, report.Reason)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "could not be read") {
		t.Errorf("expected a note that the lock notes could not be read, got %v", report.Notes)
	}
}

func TestAMwBdCloseThatWaitedOutTheBdLockLeavesANoteAndALaterCloseTakesItAway(t *testing.T) {
	ctx := context.Background()
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 4091466, Argv: []string{"bd", "-C", quietVault, "list"}})
	locks := &apptest.FakeBdLockNotes{}
	bd := apptest.NewFakeBd("f-mol-1.1")
	mwBd := func() (int, error) {
		return application.BdRetry{
			Program: bd, Quiet: procs, Turn: apptest.NewFakeBdTurn(), Dir: quietVault, LockNotes: locks,
			Wait: func(context.Context, time.Duration) error { return nil }, QuietWait: 3 * time.Second,
		}.Run(ctx, []string{"-C", quietVault, "close", "f-mol-1.1", "--reason", "done"})
	}

	if exit, err := mwBd(); err == nil || exit == 0 {
		t.Fatalf("expected the close to fail on the lock, got exit %d, err %v", exit, err)
	}
	lost, _ := locks.Lost(ctx, []string{"f-mol-1.1"})
	if !strings.Contains(lost["f-mol-1.1"], "pid 4091466") {
		t.Fatalf("expected a note naming the process the close waited for, got %v", lost)
	}

	for _, pid := range procs.Alive() {
		procs.Stop(ctx, pid)
	}
	if exit, err := mwBd(); err != nil || exit != 0 {
		t.Fatalf("expected the close to go through once the host was quiet, got exit %d, err %v", exit, err)
	}
	if lost, _ := locks.Lost(ctx, []string{"f-mol-1.1"}); len(lost) != 0 {
		t.Errorf("expected the note taken away by the close that went through, got %v", lost)
	}
}

func TestAMwBdThatIsNotACloseLeavesNoNoteForTheBdLock(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 77, Argv: []string{"bd", "-C", quietVault, "update", "mw-1"}})
	locks := &apptest.FakeBdLockNotes{}
	application.BdRetry{
		Program: apptest.NewFakeBd("f-mol-1.1"), Quiet: procs, Turn: apptest.NewFakeBdTurn(), Dir: quietVault, LockNotes: locks,
		Wait: func(context.Context, time.Duration) error { return nil }, QuietWait: 3 * time.Second,
	}.Run(context.Background(), []string{"-C", quietVault, "comment", "f-mol-1.1", "text"})
	if lost, _ := locks.Lost(context.Background(), []string{"f-mol-1.1"}); len(lost) != 0 {
		t.Errorf("expected no note for a comment, got %v", lost)
	}
}

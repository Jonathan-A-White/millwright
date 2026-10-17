package application_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aMovedTargetLanding is a landing whose target branch holds Behind commits the
// story's branch lacks (it was cut before them), and whose merge of the branch
// makes a merge commit, so that the merged result is tested.
type aMovedTargetLanding struct {
	fakeRetryLanding
	Behind int
}

func (l *aMovedTargetLanding) Ahead(_ context.Context, _, branch, _ string) (int, error) {
	if branch == application.StartPoint("origin", "main") {
		return l.Behind, nil
	}
	return l.AheadCount, nil
}

func (*aMovedTargetLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{Commit: "0123456789abcdef", Before: "fedcba9876543210"}, nil
}

// aCloseOutOnAMovedTarget closes out a finished story whose branch was cut
// behind commits of its target, with the rig's tests giving passes in turn:
// the first is the worktree's, the second the merged result's.
func aCloseOutOnAMovedTarget(t *testing.T, behind int, passes []bool) (application.NextReport, error, *scriptedChecks) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aMovedTargetLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}, Behind: behind}
	checks := &scriptedChecks{passes: passes}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checks, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return report, err, checks
}

// A branch cut before a commit on its target that fixes a test fails that test
// in its own worktree; the merged result has the fix (mw-gq6.552).
func TestAStoryCutBeforeATestFixOnItsTargetLandsOnTheMergedResult(t *testing.T) {
	report, err, checks := aCloseOutOnAMovedTarget(t, 3, []bool{false, true})

	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed on the merged result, got err %v and %+v", err, report)
	}
	if report.Reason == application.ReasonTestsFail {
		t.Errorf("expected no tests-fail refusal, got %+v", report)
	}
	if checks.runs != 2 {
		t.Errorf("expected the rig's tests run in the worktree and on the merged result, got %d runs", checks.runs)
	}
	said := strings.Join(report.Notes, "\n")
	if !strings.Contains(said, "3 commits") || !strings.Contains(said, "merged result") {
		t.Errorf("expected a note that the worktree failure went on to the merged result, got %q", said)
	}
}

// A branch whose own code breaks a test is still refused, on the merged result.
func TestAStoryOnAMovedTargetWhoseOwnCodeBreaksATestIsRefusedOnTheMergedResult(t *testing.T) {
	report, err, checks := aCloseOutOnAMovedTarget(t, 3, []bool{false})

	if err == nil || report.Landed || report.Reason != application.ReasonMergedTestsFail {
		t.Fatalf("expected a merged-tests-fail refusal, got err %v and %+v", err, report)
	}
	if checks.runs != 2 {
		t.Errorf("expected the rig's tests run in the worktree and on the merged result, got %d runs", checks.runs)
	}
	if !strings.Contains(report.Why, "failed on the merged result") {
		t.Errorf("expected the refusal to be of the merged result, got %q", report.Why)
	}
}

// A branch whose target has not moved since it was cut gates once, as ever.
func TestAStoryWhoseTargetHasNotMovedIsRefusedOnItsWorktreeRunAlone(t *testing.T) {
	report, err, checks := aCloseOutOnAMovedTarget(t, 0, []bool{false, true})

	if err == nil || report.Landed || report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", err, report)
	}
	if checks.runs != 1 {
		t.Errorf("expected one run of the rig's tests, got %d", checks.runs)
	}
}

// mw check, which only reads, still refuses a failing worktree run, and says the
// target has commits the branch was cut before.
func TestCheckOfAStoryCutBeforeCommitsOfItsTargetSaysSoWhenItsTestsFail(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	lands := &aMovedTargetLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}, Behind: 3}
	checks := &scriptedChecks{passes: []bool{false}}
	report, err := application.Check{
		Tracker: tracker, Landing: lands, Checks: checks,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard,
	}.Run(context.Background(), "mw-x.1")

	if err == nil || report.Passed() || len(report.Refusals) == 0 || report.Refusals[0].Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", err, report)
	}
	if !strings.Contains(report.Refusals[0].Why, "3 commits this branch was cut before") {
		t.Errorf("expected the refusal to name the commits the branch lacks, got %q", report.Refusals[0].Why)
	}
}

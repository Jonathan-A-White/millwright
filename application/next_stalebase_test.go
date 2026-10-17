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
type aMovedTargetLanding struct{ fakeRetryLanding }

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
	lands := &aMovedTargetLanding{fakeRetryLanding{AheadCount: 1, BehindCount: behind}}
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

// aCheckOnAMovedTarget is mw check of a story cut behind commits of its target,
// with the rig's tests giving passes in turn: the worktree's, then the target
// merged in.
func aCheckOnAMovedTarget(t *testing.T, passes []bool) (application.CheckReport, error, *scriptedChecks, *aMergeCountingLanding) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	lands := &aMergeCountingLanding{aMovedTargetLanding: aMovedTargetLanding{fakeRetryLanding{AheadCount: 1, BehindCount: 3}}}
	checks := &scriptedChecks{passes: passes}
	report, err := application.Check{
		Tracker: tracker, Landing: lands, Checks: checks,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return report, err, checks, lands
}

// mw check, which only reads, runs the tests once more with the target merged
// in a landing worktree it takes away again, and refuses only if they fail there
// too (mw-gq6.553).
func TestCheckOfAStoryCutBeforeATestFixOnItsTargetPasses(t *testing.T) {
	report, err, checks, lands := aCheckOnAMovedTarget(t, []bool{false, true})

	if err != nil || !report.Passed() {
		t.Fatalf("expected the check passed on the merged code, got err %v and %+v", err, report)
	}
	if checks.runs != 2 || lands.opened != 1 || lands.closed != 1 {
		t.Errorf("expected two runs and a landing worktree opened and closed, got %d runs, %d opened, %d closed", checks.runs, lands.opened, lands.closed)
	}
}

func TestCheckOfAStoryOnAMovedTargetWhoseOwnCodeBreaksATestRefusesOnTheMergedCode(t *testing.T) {
	report, err, _, _ := aCheckOnAMovedTarget(t, []bool{false})

	if err == nil || report.Passed() || len(report.Refusals) == 0 || report.Refusals[0].Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", err, report)
	}
	if !strings.Contains(report.Refusals[0].Why, "main merged into the branch") {
		t.Errorf("expected the refusal to be of the merged code, got %q", report.Refusals[0].Why)
	}
}

// aBatchRigsRules is the file of a rig that lands in batches.
type aBatchRigsRules struct{}

func (aBatchRigsRules) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{Landing: domain.LandingSettings{Mode: domain.LandingBatch}}, nil
}

// aMergeCountingLanding counts the landing worktrees it opens and closes and
// the merges made in them, on top of a target that has moved.
type aMergeCountingLanding struct {
	aMovedTargetLanding
	opened, closed, merged int
}

func (l *aMergeCountingLanding) OpenLanding(context.Context, string, string) (string, error) {
	l.opened++
	return "/rigs/millwright/.landing-1", nil
}

func (l *aMergeCountingLanding) CloseLanding(context.Context, string, string) error {
	l.closed++
	return nil
}

func (l *aMergeCountingLanding) Merge(ctx context.Context, dir, branch string) (application.Landed, error) {
	l.merged++
	return l.aMovedTargetLanding.Merge(ctx, dir, branch)
}

// aBatchRigCloseOutOnAMovedTarget closes out a finished story on a rig that
// lands in batches, its branch cut behind commits of its target, with the rig's
// tests giving passes in turn.
func aBatchRigCloseOutOnAMovedTarget(t *testing.T, behind int, passes []bool) (application.NextReport, error, *scriptedChecks, *aMergeCountingLanding) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aMergeCountingLanding{aMovedTargetLanding: aMovedTargetLanding{fakeRetryLanding{AheadCount: 1, BehindCount: behind}}}
	checks := &scriptedChecks{passes: passes}
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands, Rules: aBatchRigsRules{},
		Checks: checks, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"lampas": "/rigs/lampas"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	return report, err, checks, lands
}

// On a rig that lands in batches, a branch cut before a commit on its target
// that fixes a test fails that test in its own worktree too; the target merged
// in has the fix, so the story is queued, not refused (mw-gq6.553). It fails
// with the merge taken out: then the worktree's one failing run is a refusal.
func TestABatchRigStoryCutBeforeATestFixOnItsTargetIsQueued(t *testing.T) {
	report, err, checks, lands := aBatchRigCloseOutOnAMovedTarget(t, 3, []bool{false, true})

	if err != nil || !report.Queued {
		t.Fatalf("expected the story queued for its batch, got err %v and %+v", err, report)
	}
	if report.Reason == application.ReasonTestsFail {
		t.Errorf("expected no tests-fail refusal, got %+v", report)
	}
	if checks.runs != 2 || lands.opened != 1 || lands.closed != 1 || lands.merged != 1 {
		t.Errorf("expected the tests run in the worktree and once on the target merged in a landing worktree taken away again, got %d runs, %d opened, %d closed, %d merged",
			checks.runs, lands.opened, lands.closed, lands.merged)
	}
	said := strings.Join(report.Notes, "\n")
	if !strings.Contains(said, "3 commits") || !strings.Contains(said, "merged") {
		t.Errorf("expected a note that the worktree failure went on to the target merged in, got %q", said)
	}
}

// On a rig that lands in batches, a story whose own code breaks a test is still
// refused tests-fail, on the merged code, naming the failing test as ever.
func TestABatchRigStoryWhoseOwnCodeBreaksATestIsRefusedTestsFailOnTheMergedCode(t *testing.T) {
	report, err, checks, _ := aBatchRigCloseOutOnAMovedTarget(t, 3, []bool{false})

	if err == nil || report.Queued || report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", err, report)
	}
	if checks.runs != 2 {
		t.Errorf("expected the rig's tests run in the worktree and on the merged code, got %d runs", checks.runs)
	}
	if !strings.Contains(report.Why, "merged") {
		t.Errorf("expected the refusal to be of the merged code, got %q", report.Why)
	}
}

// A batch-rig story whose target has not moved gates once, as ever.
func TestABatchRigStoryWhoseTargetHasNotMovedIsRefusedOnItsWorktreeRunAlone(t *testing.T) {
	report, err, checks, lands := aBatchRigCloseOutOnAMovedTarget(t, 0, []bool{false, true})

	if err == nil || report.Queued || report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected a tests-fail refusal, got err %v and %+v", err, report)
	}
	if checks.runs != 1 || lands.opened != 0 {
		t.Errorf("expected one run of the rig's tests and no landing worktree, got %d runs and %d opened", checks.runs, lands.opened)
	}
}

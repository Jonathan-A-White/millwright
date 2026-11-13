package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A story filed from another story's path is a different story: the source's
// callers_ok is a reason about the source's files, and its size and bump are the
// source's own, so none is copied, as mw path set --from copies none
// (mw-gq6.1227, after mw-gq6.1195).

const theSourcesCallersOK = "adds one new branch; the 44 callers are unchanged"

func theSourceStory() domain.Story {
	return domain.Story{ID: "mw-x.1", Title: "A landed story", Overrides: domain.Path{
		CallersOK: theSourcesCallersOK,
		Size:      domain.SizeM,
		Bump:      "minor",
		Files:     "application/next.go",
	}}
}

func noneOfTheSourcesKeys(t *testing.T, path domain.Path) {
	t.Helper()
	if path.CallersOK != "" {
		t.Errorf("expected no callers_ok on the filed story, got %q", path.CallersOK)
	}
	if path.Size != "" {
		t.Errorf("expected no size copied from the source, got %q", path.Size)
	}
	if path.Bump != "" {
		t.Errorf("expected no bump copied from the source, got %q", path.Bump)
	}
}

func TestAnUnmetACBugCarriesNoCallersOK(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", theSourceStory())
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", "Done, but AC 2 NOT met: the test does not run yet.\nFor the rig memory: nothing"))
	before := tracker.Stories()
	lands := &fakeRetryLanding{}
	_, err := application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Mailbox: apptest.NewFakeMailbox(),
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	mustDo(t, err)

	var bug []application.StoryDetail
	for _, d := range theTestersFiled(t, tracker, before) {
		if strings.Contains(d.Story.Title, "AC") {
			bug = append(bug, d)
		}
	}
	if len(bug) != 1 {
		t.Fatalf("expected 1 unmet-AC bug filed, got %d: %+v", len(bug), bug)
	}
	noneOfTheSourcesKeys(t, bug[0].Merged())
	if got := bug[0].Merged().Files; got != "application/next.go" {
		t.Errorf("expected the landed story's files kept on the unmet-AC bug, got %q", got)
	}
}

func TestAMainRedBugCarriesNoCallersOK(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", theSourceStory())
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	named := &namedTestsThat{run: failingOnMain("--- FAIL: TestA (0.01s)\n--- FAIL: TestB (0.01s)\nFAIL\n")}
	before := tracker.Stories()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: twoFailingTests{}, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		NamedChecks: named, MainAloneBound: 0,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Seat:     "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red, got %q (%v)", report.Reason, err)
	}

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 || !strings.Contains(filed[0].Story.Title, "main is red") {
		t.Fatalf("expected 1 main-red bug filed, got %d: %+v", len(filed), filed)
	}
	noneOfTheSourcesKeys(t, filed[0].Merged())
}

func TestATesterStoryCarriesNoCallersOK(t *testing.T) {
	tracker, rules := aBatchEpicToTest(t)
	before := tracker.Stories()
	trial := application.TesterTrial{Rigs: []string{"lampas"}, Until: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC),
		Model: domain.ModelSonnet, Effort: domain.EffortHigh}

	path := domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelOpus, Effort: domain.EffortHigh,
		CallersOK: theSourcesCallersOK, Size: domain.SizeM, Bump: "minor", Files: "src/page.ts"}
	aBatchLandedOnPathWithTesters(t, trial, tracker, rules, path, "mw-e.1")

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 || !application.IsTesterStory(filed[0]) {
		t.Fatalf("expected 1 Tester story filed, got %d: %+v", len(filed), filed)
	}
	noneOfTheSourcesKeys(t, filed[0].Merged())
	if got := filed[0].Merged().Files; got != "" {
		t.Errorf("expected no files on the Tester story, got %q", got)
	}
}

// A story filed from a Tester's finding takes the landed story's rig, branch,
// host, model, effort and formula, and none of its callers_ok, size or bump
// (mw-gq6.1227).
func TestAStoryFiledFromATestersFindingCarriesNoCallersOK(t *testing.T) {
	tracker, rules := testerFileEpic(t)
	mustDo(t, tracker.SetStoryMetadata(context.Background(), "mw-e.1", map[string]string{
		domain.CallersOKField: theSourcesCallersOK, "size": string(domain.SizeM), "bump": "minor"}))
	filer := application.TesterFiler{Tracker: tracker, Rules: rules, Out: io.Discard}

	filed, err := filer.File(context.Background(), "mw-e.2", testerFinding())
	mustDo(t, err)
	story, err := tracker.ShowStory(context.Background(), filed.ID)
	mustDo(t, err)
	landed, err := tracker.ShowStory(context.Background(), "mw-e.1")
	mustDo(t, err)
	if landed.Merged().CallersOK == "" || landed.Merged().Size == "" || landed.Merged().Bump == "" {
		t.Fatalf("the fixture's landed story has no callers_ok, size and bump to copy: %+v", landed.Merged())
	}
	noneOfTheSourcesKeys(t, story.Merged())
}

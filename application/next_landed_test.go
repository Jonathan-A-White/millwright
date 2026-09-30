package application_test

import (
	"context"
	"io"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A checkStub and a slotStub stand in for the rig's tests and the merge slot:
// a story already landed reaches neither.
type checkStub struct{}

func (checkStub) Run(context.Context, string, string) (application.Checked, error) {
	return application.Checked{}, nil
}

type slotStub struct{}

func (slotStub) Take(context.Context, string, string) (application.Holding, error) {
	return nil, nil
}

// A landing whose run=landed write failed leaves only its ledger line and a
// merged branch. The later mw next that closes it from that line writes the
// marker the Verify card is built from, before it closes.
func TestNextWritesRunLandedWhenItClosesAStoryFromItsLedgerLine(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story"})
	vault := newFakeVault()
	mustDo(t, vault.AppendToLedger(ctx, "builder", "| 2026-09-30 | mw-x.1 | landed on main | 1 | |"))

	if was, _ := tracker.StoryState(ctx, "mw-x.1", application.RunState); was != "" {
		t.Fatalf("expected the story to carry no run state to begin with, got %q", was)
	}
	lands := &fakeRetryLanding{} // Ahead is 0: the branch is merged
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if err != nil {
		t.Fatalf("mw next: %v", err)
	}
	if !report.Closed || !report.LandedEarlier {
		t.Fatalf("expected the story closed as landed by an earlier run, got %+v", report)
	}
	if was, _ := tracker.StoryState(ctx, "mw-x.1", application.RunState); was != application.RunLanded {
		t.Fatalf("expected the story to carry %s=%s once closed, got %q", application.RunState, application.RunLanded, was)
	}
}

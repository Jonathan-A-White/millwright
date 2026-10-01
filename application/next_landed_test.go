package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
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

// closeFailsAtFirst is a tracker whose close fails, the way the tracker's
// server failed mw-a0ih0.11's (mw-gq6.191), the first fails times it is asked,
// and goes through after that.
type closeFailsAtFirst struct {
	*apptest.FakeTracker
	fails int
	asked int
}

func (c *closeFailsAtFirst) CloseStory(ctx context.Context, id, reason string) error {
	c.asked++
	if c.asked <= c.fails {
		return errors.New("exit status 1: Error: failed to open database: Dolt server unreachable at 10.88.0.2:3307: dial tcp 10.88.0.2:3307: i/o timeout")
	}
	return c.FakeTracker.CloseStory(ctx, id, reason)
}

// aLandingToClose is a story whose landing is done, recorded run=landed, with
// only the close left, and the mw next that closes it on the given tracker.
func aLandingToClose(t *testing.T, tracker *closeFailsAtFirst) application.Next {
	t.Helper()
	ctx := context.Background()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A landed story"})
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.1", application.RunState, application.RunLanded, "landed on main"))
	lands := &fakeRetryLanding{}
	return application.Next{
		Tracker: tracker, Vault: newFakeVault(), Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{},
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}
}

func TestNextTriesAFailedCloseAgainAndClosesTheLandedStory(t *testing.T) {
	ctx := context.Background()
	tracker := &closeFailsAtFirst{FakeTracker: apptest.NewFakeTracker(), fails: 2}
	next := aLandingToClose(t, tracker)
	next.PushTries = 3

	report, err := next.Run(ctx, "mw-x.1")
	if err != nil {
		t.Fatalf("expected the close to go through on its third try, got %v", err)
	}
	if !report.Closed || report.NotClosed != "" {
		t.Fatalf("expected the story closed, got %+v", report)
	}
	if tracker.asked != 3 {
		t.Fatalf("expected the close asked 3 times, got %d", tracker.asked)
	}
	if detail, _ := tracker.ShowStory(ctx, "mw-x.1"); detail.Status != apptest.StatusClosed {
		t.Fatalf("expected the story closed in the tracker, got %q", detail.Status)
	}
}

func TestNextReportsALandedStoryItCouldNotCloseAfterEveryTry(t *testing.T) {
	ctx := context.Background()
	tracker := &closeFailsAtFirst{FakeTracker: apptest.NewFakeTracker(), fails: 100}
	next := aLandingToClose(t, tracker)
	next.PushTries = 3

	report, err := next.Run(ctx, "mw-x.1")
	if err == nil || !strings.Contains(err.Error(), "landed and still open") {
		t.Fatalf("expected the close-out to say the story is landed and still open, got %v", err)
	}
	if tracker.asked != 3 {
		t.Fatalf("expected the close asked 3 times, got %d", tracker.asked)
	}
	if report.Closed || !strings.Contains(report.NotClosed, "i/o timeout") || !strings.Contains(report.NotClosed, "3 tries") {
		t.Fatalf("expected the report to say the close failed on every one of its 3 tries, got %+v", report)
	}
	if was, _ := tracker.StoryState(ctx, "mw-x.1", application.RunState); was != application.RunLanded {
		t.Fatalf("expected the story still recorded %s=%s for dispatch to close, got %q", application.RunState, application.RunLanded, was)
	}
}

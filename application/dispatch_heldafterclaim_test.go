package application_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// heldTracker is a tracker on which the Mayor holds the story a moment after
// this dispatch claimed it (mw-gq6.878): right after the claim, or while the
// formula is poured.
type heldTracker struct {
	*apptest.FakeTracker
	afterClaim, whilePouring bool
}

func (h *heldTracker) ClaimStory(ctx context.Context, id string) error {
	if err := h.FakeTracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	if h.afterClaim {
		return h.FakeTracker.HoldStory(ctx, id)
	}
	return nil
}

func (h *heldTracker) PourFormula(ctx context.Context, formula, id, title string, bug bool) (application.Molecule, error) {
	molecule, err := h.FakeTracker.PourFormula(ctx, formula, id, title, bug)
	if err == nil && h.whilePouring {
		err = h.FakeTracker.HoldStory(ctx, id)
	}
	return molecule, err
}

// mw-gq6.878: a story the Mayor held after dispatch claimed it is not worked.
// No session starts, the claim is given back, and the hold stands: the story is
// held and nobody's, as the Mayor left it, and not running.
func TestDispatchStartsNoSessionForAStoryHeldAfterItsClaim(t *testing.T) {
	for _, tc := range []struct {
		name string
		held heldTracker
	}{
		{"held right after the claim", heldTracker{afterClaim: true}},
		{"held while the formula is poured", heldTracker{whilePouring: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dispatch, tracker, _, runner, _ := aFactory(t)
			tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"})
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
			held := tc.held
			held.FakeTracker = tracker
			dispatch.Tracker = &held

			report, err := dispatch.Run(ctx)
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(report.Started) != 0 || len(runner.Names()) != 0 {
				t.Fatalf("expected no session for a held story, got started %+v and sessions %v", report.Started, runner.Names())
			}
			if len(report.Failed) != 0 {
				t.Fatalf("a story the Mayor held is not a failed start, got %+v", report.Failed)
			}
			if len(report.Passed) != 1 || report.Passed[0].StoryID != "mw-gq6.1" || report.Passed[0].Gate != application.HeldAfterClaimGate {
				t.Fatalf("expected the story passed over for %q, got %+v", application.HeldAfterClaimGate, report.Passed)
			}
			detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
			if err != nil {
				t.Fatalf("showing the story: %v", err)
			}
			if !detail.Held() || detail.Assignee != "" || !detail.LeaseExpires.IsZero() {
				t.Fatalf("expected the story held and unclaimed, got status %q, assignee %q, lease %v", detail.Status, detail.Assignee, detail.LeaseExpires)
			}
			for _, label := range detail.Labels {
				if label == application.RunState+":"+application.RunRunning {
					t.Fatalf("expected the story not to read as running, labels %v", detail.Labels)
				}
			}
		})
	}
}

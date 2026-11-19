package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// mw-gq6.1498: a close-out that waits (for the gate lock, in the report that
// filed this) leaves its mark on the story in the tracker, and a dispatch that
// runs meanwhile, on this host or another, takes the story for nothing.

// registerNextClosingOutSteps registers mw-gq6.1498's steps of features/next.feature.
func registerNextClosingOutSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the claim on "([^"]*)" was given back by its refused close-out$`, func(id string) error {
		return c.tracker.ReleaseClaim(context.Background(), id)
	})
	ctx.Given(`^a dispatch tick runs once the close-out of "([^"]*)" has left its mark$`, c.aTickRunsOnceTheCloseOutHasMarked)
	ctx.Then(`^that dispatch started nothing and named "([^"]*)" as being landed by the Mayor$`, c.thatDispatchNamedItAsLandedByTheMayor)
	ctx.Then(`^the story "([^"]*)" still records attempt (\d+) and carries no closing-out mark$`, c.theStoryKeepsItsAttemptsAndHasNoMark)
}

// markedThenTick is a tracker that runs a hook once, the moment the close-out
// writes its mark on a story: the close-out is then under way, as it is while
// it waits for the gate lock.
type markedThenTick struct {
	application.WorkTracker
	hook func()
}

func (m *markedThenTick) SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error {
	err := m.WorkTracker.SetStoryMetadata(ctx, id, fields)
	if fields[application.ClosingOutField] != "" && m.hook != nil {
		hook := m.hook
		m.hook = nil
		hook()
	}
	return err
}

func (c *nextContext) aTickRunsOnceTheCloseOutHasMarked(id string) error {
	c.duringCloseOut = func() {
		worktrees := rig.New(rig.WithProgram(c.gitProgram))
		boot := application.SeatBoot{Vault: vault.New(c.vault), Harness: claude.New(), Seat: nextSeat, Host: nextHost}
		c.dispatchReport, c.dispatchErr = application.Dispatch{
			Tracker: c.tracker, Worktrees: worktrees, Landing: worktrees, Runner: c.runner, Memory: c.tracker,
			Boot: boot, Host: nextHost, Cap: 1, Rigs: map[string]string{"millwright": c.rig},
			Now: func() time.Time { return time.Date(2026, 9, 18, 12, 1, 0, 0, time.UTC) },

			RetryFromBranch: !c.fromMain,
			Rebases:         worktrees,
		}.Run(context.Background())
	}
	return nil
}

func (c *nextContext) thatDispatchNamedItAsLandedByTheMayor(id string) error {
	if c.dispatchErr != nil {
		return fmt.Errorf("the dispatch failed: %w", c.dispatchErr)
	}
	if len(c.dispatchReport.Started) != 0 {
		return fmt.Errorf("expected the dispatch to start nothing, it started %+v", c.dispatchReport.Started)
	}
	for _, passed := range c.dispatchReport.Passed {
		if passed.StoryID == id && passed.Gate == "closing-out" && strings.Contains(passed.Why, "landed by the Mayor") {
			return nil
		}
	}
	return fmt.Errorf("expected %s passed over at the closing-out gate, landed by the Mayor, got %+v (report %+v)", id, c.dispatchReport.Passed, c.dispatchReport)
}

func (c *nextContext) theStoryKeepsItsAttemptsAndHasNoMark(id string, attempts int) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Attempts != attempts {
		return fmt.Errorf("expected %s to record attempt %d, got %d", id, attempts, detail.Attempts)
	}
	if !detail.ClosingOut.Until.IsZero() {
		return fmt.Errorf("expected %s to carry no closing-out mark once its close-out ended, got %+v", id, detail.ClosingOut)
	}
	return nil
}

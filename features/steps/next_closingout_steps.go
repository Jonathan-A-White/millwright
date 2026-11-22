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
	ctx.Given(`^a dispatch tick that read "([^"]*)" before its mark runs once the close-out of "([^"]*)" has left its mark$`, c.aStaleTickRunsOnceTheCloseOutHasMarked)
	ctx.Given(`^attempt 2 of "([^"]*)" is started once the close-out has read the story$`, c.attemptTwoStartsAfterTheCloseOutRead)
	ctx.Then(`^the report's reason names attempts (\d+) and (\d+)$`, c.theReasonNamesTheAttempts)
	ctx.Then(`^the session of "([^"]*)" was not closed$`, c.theSessionWasNotClosed)
	ctx.Then(`^the formula poured for "([^"]*)" is still open$`, c.theFormulaIsStillOpen)
	ctx.Then(`^the story "([^"]*)" still records attempt (\d+) and carries no closing-out mark$`, c.theStoryKeepsItsAttemptsAndHasNoMark)
}

// markedThenTick is a tracker that runs a hook once, the moment the close-out
// writes its mark on a story: the close-out is then under way, as it is while
// it waits for the gate lock.
type markedThenTick struct {
	application.WorkTracker
	hook func()
	// afterRead runs once, after the close-out's first look at the story's run
	// state, which it takes right after it has read the story (mw-gq6.1656).
	afterRead func()
}

func (m *markedThenTick) StoryState(ctx context.Context, id, dimension string) (string, error) {
	state, err := m.WorkTracker.StoryState(ctx, id, dimension)
	if dimension == application.RunState && m.afterRead != nil {
		after := m.afterRead
		m.afterRead = nil
		after()
	}
	return state, err
}

// staleReadyTracker is a tracker whose ready list is the one a dispatch read
// before the close-out wrote its mark: no story in it carries a closing-out
// mark. Every other read is fresh.
type staleReadyTracker struct{ application.WorkTracker }

func (s staleReadyTracker) ReadyForHost(ctx context.Context, host string) ([]application.StoryDetail, error) {
	ready, err := s.WorkTracker.ReadyForHost(ctx, host)
	for i := range ready {
		ready[i].ClosingOut = application.ClosingOut{}
	}
	return ready, err
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
	return c.tickRunsOnceMarked(c.tracker)
}

// aStaleTickRunsOnceTheCloseOutHasMarked is that tick, but its dispatch read
// the ready list before the mark was there (mw-gq6.1656): only a second look,
// just before the claim, sees it.
func (c *nextContext) aStaleTickRunsOnceTheCloseOutHasMarked(_, _ string) error {
	return c.tickRunsOnceMarked(staleReadyTracker{c.tracker})
}

func (c *nextContext) tickRunsOnceMarked(tracker application.WorkTracker) error {
	c.duringCloseOut = func() {
		worktrees := rig.New(rig.WithProgram(c.gitProgram))
		boot := application.SeatBoot{Vault: vault.New(c.vault), Harness: claude.New(), Seat: nextSeat, Host: nextHost}
		c.dispatchReport, c.dispatchErr = application.Dispatch{
			Tracker: tracker, Worktrees: worktrees, Landing: worktrees, Runner: c.runner, Memory: c.tracker,
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

// attemptTwoStartsAfterTheCloseOutRead is a dispatch of another host claiming
// the story and starting attempt 2 under its name once the close-out has read
// it as attempt 1 (mw-gq6.1656): the story's attempt and the time of its claim
// move on, and a session of its name is running.
func (c *nextContext) attemptTwoStartsAfterTheCloseOutRead(id string) error {
	c.afterStoryRead = func() {
		ctx := context.Background()
		_ = c.tracker.SetStoryMetadata(ctx, id, map[string]string{
			application.AttemptsField:  "2",
			application.ClaimedAtField: time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		})
		_ = c.runner.Start(ctx, application.SessionSpec{Name: application.SessionName(id), Command: []string{"claude"}})
	}
	return nil
}

func (c *nextContext) theReasonNamesTheAttempts(first, second int) error {
	for _, n := range []int{first, second} {
		if !strings.Contains(c.report.Why, fmt.Sprintf("attempt %d", n)) {
			return fmt.Errorf("expected the reason to name attempt %d, got %q", n, c.report.Why)
		}
	}
	return nil
}

func (c *nextContext) theSessionWasNotClosed(id string) error {
	name := application.SessionName(id)
	for _, closed := range c.runner.Closed() {
		if closed == name {
			return fmt.Errorf("expected the session %s to be left running, but it was closed", name)
		}
	}
	return nil
}

func (c *nextContext) theFormulaIsStillOpen(id string) error {
	ctx := context.Background()
	detail, err := c.tracker.ShowStory(ctx, id)
	if err != nil {
		return err
	}
	open, err := c.tracker.OpenSteps(ctx, detail.Molecule.RootID)
	if err != nil {
		return err
	}
	if len(open) == 0 {
		return fmt.Errorf("expected the formula of %s to be left as it was, with a step open, but no step is open", id)
	}
	return nil
}

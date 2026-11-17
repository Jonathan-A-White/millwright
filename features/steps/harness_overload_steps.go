package steps

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of features/harness_overload.feature (mw-gq6.1382): a close-out the
// harness stopped, then the dispatch ticks that give the story back, run in
// next.feature's world.

// registerHarnessOverloadSteps registers them with nextContext.
func registerHarnessOverloadSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the story "([^"]*)" was claimed at "([^"]*)" for its attempt (\d+)$`, c.theStoryWasClaimedAt)
	ctx.When(`^the pane of "([^"]*)" is dead$`, c.thePaneIsDead)
	ctx.When(`^a dispatch tick runs at "([^"]*)" with room for (\d+) sessions?$`, c.aDispatchTickRunsAt)
	ctx.Then(`^the story "([^"]*)" is open with no claim and no run state label$`, c.theStoryIsOpenWithNoRunState)
	ctx.Then(`^the story "([^"]*)", untouched since "([^"]*)", is named by no refused-open alarm at "([^"]*)"$`, c.theStoryIsNamedByNoRefusedAlarm)
	ctx.Then(`^the dispatch started "([^"]*)" again as attempt (\d+)$`, c.theDispatchStartedAgainAs)
}

func (c *nextContext) theStoryWasClaimedAt(id, at string, attempt int) error {
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		return err
	}
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{
		application.ClaimedAtField: at,
		application.AttemptsField:  strconv.Itoa(attempt),
	})
}

// aDispatchTickRunsAt is one tick of mw dispatch on the clock at, with the
// scenario's real rig and vault, that may start room sessions.
func (c *nextContext) aDispatchTickRunsAt(at string, room int) error {
	now, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	c.tracker.Clock = func() time.Time { return now }
	worktrees := rig.New(rig.WithProgram(c.gitProgram), rig.WithNpm("true"))
	boot := application.SeatBoot{
		Vault: vault.New(c.vault), Harness: claude.New(), Seat: nextSeat, Host: nextHost,
	}
	report, err := application.Dispatch{
		Tracker:   c.tracker,
		Worktrees: worktrees,
		Landing:   worktrees,
		Runner:    c.runner,
		Memory:    c.tracker,
		Boot:      boot,
		Host:      nextHost,
		Cap:       room,
		Rigs:      map[string]string{c.rigKey(): c.rig},
		Now:       func() time.Time { return now },

		RetryFromBranch: !c.fromMain,
		Rebases:         worktrees,
	}.Run(context.Background())
	c.dispatchReport, c.err = report, err
	return err
}

func (c *nextContext) theStoryIsOpenWithNoRunState(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" {
		return fmt.Errorf("expected %s open with no claim, got status %q held by %q (dispatch said: %+v)", id, detail.Status, detail.Assignee, c.dispatchReport)
	}
	for _, label := range detail.Labels {
		if len(label) > len(application.RunState) && label[:len(application.RunState)+1] == application.RunState+":" {
			return fmt.Errorf("expected %s to carry no run state, it carries %q", id, label)
		}
	}
	return nil
}

func (c *nextContext) theStoryIsNamedByNoRefusedAlarm(id, since, at string) error {
	touched, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return err
	}
	now, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	if err := c.tracker.SetUpdated(id, touched); err != nil {
		return err
	}
	work, err := c.tracker.WorkInHand(context.Background())
	if err != nil {
		return err
	}
	for _, found := range application.OpenRefusedStories(work, now) {
		if found.Story.Story.ID == id {
			return fmt.Errorf("expected no refused-open alarm for %s at %s, got: %s", id, at, found.Alarm().Text)
		}
	}
	return nil
}

func (c *nextContext) theDispatchStartedAgainAs(id string, attempt int) error {
	i := slices.IndexFunc(c.dispatchReport.Started, func(s application.Started) bool { return s.StoryID == id })
	if i < 0 {
		return fmt.Errorf("expected the tick to start %s again, got %+v (dispatch said: %v)", id, c.dispatchReport, c.err)
	}
	if got := c.dispatchReport.Started[i].Attempt; got != attempt {
		return fmt.Errorf("expected %s started as attempt %d, got %d", id, attempt, got)
	}
	return nil
}

// thePaneIsDead is the session's window as a later tick finds it: the harness
// gone, the pane still there to be closed.
func (c *nextContext) thePaneIsDead(id string) error {
	c.runner.Exit(application.SessionName(id), 0)
	return nil
}

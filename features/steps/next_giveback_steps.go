package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerNextGiveBackSteps registers mw-gq6.393's steps of features/next.feature:
// the claim give-back of an open-steps stop meeting a passing fault.
func registerNextGiveBackSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the next (\d+) give-backs of the claim on "([^"]*)" fail, saying: (.+)$`, c.theNextGiveBacksFail)

	ctx.Then(`^the story "([^"]*)" is open and unclaimed$`, c.theStoryIsOpenAndUnclaimed)
	ctx.Then(`^the story "([^"]*)" keeps its claim$`, c.theStoryKeepsItsClaim)
}

// theNextGiveBacksFail makes the tracker turn the next n give-backs down the way
// bd does a Dolt server it could not reach: a passing fault of the network.
func (c *nextContext) theNextGiveBacksFail(n int, _ string, said string) error {
	faults := make([]apptest.ReleaseFault, n)
	for i := range faults {
		said := strings.TrimSpace(said)
		faults[i] = apptest.ReleaseFault{Err: &application.TransientFault{Why: said, Err: errors.New("bd update: exit status 1: " + said)}}
	}
	c.tracker.FailReleases(faults...)
	return nil
}

func (c *nextContext) theStoryIsOpenAndUnclaimed(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != apptest.StatusOpen || detail.Assignee != "" {
		return fmt.Errorf("expected %s to be open and unclaimed, it reads %s held by %q", id, detail.Status, detail.Assignee)
	}
	return nil
}

func (c *nextContext) theStoryKeepsItsClaim(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Assignee == "" {
		return fmt.Errorf("expected %s to be still claimed, but it is unclaimed (%s)", id, detail.Status)
	}
	return nil
}

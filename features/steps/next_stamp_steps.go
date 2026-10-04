package steps

import (
	"errors"
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

// registerNextStampSteps registers the chain stamp steps of features/next.feature:
// c.stamps is the queue mwClosesOut hands to mw next.
func registerNextStampSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the stamp queue refuses to take a stamp, saying: (.+)$`, c.theStampQueueRefuses)

	ctx.Then(`^one stamp is queued for the commit that landed on "([^"]*)", for the story "([^"]*)"$`, c.oneStampIsQueued)
	ctx.Then(`^no stamp is queued$`, c.noStampIsQueued)
}

func (c *nextContext) theStampQueueRefuses(said string) error {
	c.stamps.AppendErr = errors.New(said)
	return nil
}

func (c *nextContext) oneStampIsQueued(branch, story string) error {
	queued := c.stamps.Queued()
	if len(queued) != 1 {
		return fmt.Errorf("expected one stamp queued, got %d: %+v", len(queued), queued)
	}
	landed, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	got := queued[0].Stamp
	if got.Rig != "millwright" || got.Branch != branch || got.Commit != landed || got.Story != story ||
		got.Title != "The story "+story || got.Host != nextHost ||
		!got.At.Equal(time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)) {
		return fmt.Errorf("expected a stamp of millwright %s at %s for %s on %s, got %+v", branch, landed, story, nextHost, got)
	}
	return nil
}

func (c *nextContext) noStampIsQueued() error {
	if queued := c.stamps.Queued(); len(queued) != 0 {
		return fmt.Errorf("expected no stamp queued, got %+v", queued)
	}
	return nil
}

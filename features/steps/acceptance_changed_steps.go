package steps

import (
	"context"
	"time"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of features/acceptance_changed.feature (mw-gq6.509): a story whose
// acceptance changed after its claim. They hang off nextContext.

// registerAcceptanceChangedSteps registers the steps of
// features/acceptance_changed.feature.
func registerAcceptanceChangedSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the story "([^"]*)" was claimed with the acceptance:$`, c.theStoryWasClaimedWithTheAcceptance)
	ctx.Given(`^the acceptance of "([^"]*)" now reads:$`, c.theAcceptanceNowReads)
	ctx.Given(`^the Mayor wrote a closing comment on "([^"]*)" after its newest commit$`, c.theMayorWroteAClosingComment)
	ctx.Given(`^mw next is run with --rescoped-by-mayor "([^"]*)"$`, c.mwNextIsRunRescoped)
}

// theStoryWasClaimedWithTheAcceptance gives the story the acceptance and records
// it as a dispatch does at the claim.
func (c *nextContext) theStoryWasClaimedWithTheAcceptance(id string, text *godog.DocString) error {
	if err := c.tracker.SetAcceptance(id, text.Content); err != nil {
		return err
	}
	return c.tracker.SetStoryMetadata(context.Background(), id,
		map[string]string{application.AcceptanceAtClaimField: application.AcceptanceAtClaim(text.Content)})
}

// theAcceptanceNowReads is the Mayor amending the acceptance of the bead.
func (c *nextContext) theAcceptanceNowReads(id string, text *godog.DocString) error {
	return c.tracker.SetAcceptance(id, text.Content)
}

// theMayorWroteAClosingComment is a closing comment on the story, stamped after
// the commits the scenario makes. The fake tracker gives every comment one
// author, which is no Builder's, so it reads as the Mayor's.
func (c *nextContext) theMayorWroteAClosingComment(id string) error {
	return c.tracker.CommentOnStoryAt(id, "Re-scoped and done.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing",
		time.Now().Add(time.Hour))
}

// mwNextIsRunRescoped is the flag the Mayor adds to mw next.
func (c *nextContext) mwNextIsRunRescoped(why string) error {
	c.rescopedBy = why
	return nil
}

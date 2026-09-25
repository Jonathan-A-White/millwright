package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// leaseDay is the day every scenario of features/claim_lease.feature runs on;
// its steps name only the time of day.
var leaseDay = time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

// leaseWorld is what features/claim_lease.feature adds to readyContext: the
// fake tracker's clock, and what the last claim, heartbeat and reclaim said.
type leaseWorld struct {
	now       time.Time
	claimErr  error
	beatErr   error
	reclaimed bool
}

// registerClaimLeaseSteps registers the steps of features/claim_lease.feature.
// They share readyContext, whose Background steps the feature reuses, and are
// registered from InitializeReadyStoriesScenario.
func registerClaimLeaseSteps(ctx *godog.ScenarioContext, c *readyContext) {
	ctx.Given(`^the story "([^"]*)" is claimed at (\d\d:\d\d)$`, c.theStoryIsClaimedAt)
	ctx.Given(`^the story "([^"]*)" is held by "([^"]*)" with a lease until (\d\d:\d\d)$`, c.theStoryIsHeldBy)
	ctx.Given(`^the claim on "([^"]*)" is reclaimed at (\d\d:\d\d)$`, c.theClaimIsReclaimedAt)
	ctx.When(`^the story "([^"]*)" is claimed at (\d\d:\d\d)$`, c.theStoryIsClaimedAtWhen)
	ctx.When(`^the claim on "([^"]*)" is heartbeaten at (\d\d:\d\d)$`, c.theClaimIsHeartbeatenAt)
	ctx.When(`^the claim on "([^"]*)" is reclaimed at (\d\d:\d\d)$`, c.theClaimIsReclaimedAt)
	ctx.Then(`^the story "([^"]*)" holds a lease until (\d\d:\d\d)$`, c.theStoryHoldsALeaseUntil)
	ctx.Then(`^the stale claims at (\d\d:\d\d) are "([^"]*)"$`, c.theStaleClaimsAre)
	ctx.Then(`^the claim on "([^"]*)" was reclaimed$`, c.theClaimWasReclaimed)
	ctx.Then(`^the claim on "([^"]*)" was not reclaimed$`, c.theClaimWasNotReclaimed)
	ctx.Then(`^the story "([^"]*)" is open and held by nobody$`, c.theStoryIsOpenAndHeldByNobody)
	ctx.Then(`^the heartbeat fails$`, c.theHeartbeatFails)
	ctx.Then(`^the claim fails because "([^"]*)" holds the story$`, c.theClaimFailsBecauseHeld)
	ctx.Then(`^the story "([^"]*)" is held by "([^"]*)" with a lease until (\d\d:\d\d)$`, c.theStoryIsStillHeldBy)
}

// at sets the fake tracker's clock to a time of day on leaseDay.
func (c *readyContext) at(clock string) (time.Time, error) {
	when, err := time.Parse("15:04", clock)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading the time %q: %w", clock, err)
	}
	c.lease.now = leaseDay.Add(time.Duration(when.Hour())*time.Hour + time.Duration(when.Minute())*time.Minute)
	c.tracker.Clock = func() time.Time { return c.lease.now }
	return c.lease.now, nil
}

func (c *readyContext) theStoryIsClaimedAt(id, clock string) error {
	if _, err := c.at(clock); err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

func (c *readyContext) theStoryIsClaimedAtWhen(id, clock string) error {
	if _, err := c.at(clock); err != nil {
		return err
	}
	c.lease.claimErr = c.tracker.ClaimStory(context.Background(), id)
	return nil
}

func (c *readyContext) theStoryIsHeldBy(id, holder, clock string) error {
	until, err := c.at(clock)
	if err != nil {
		return err
	}
	return c.tracker.ClaimAs(id, holder, until)
}

func (c *readyContext) theClaimIsHeartbeatenAt(id, clock string) error {
	if _, err := c.at(clock); err != nil {
		return err
	}
	c.lease.beatErr = c.tracker.HeartbeatClaim(context.Background(), id)
	return nil
}

func (c *readyContext) theClaimIsReclaimedAt(id, clock string) error {
	if _, err := c.at(clock); err != nil {
		return err
	}
	reclaimed, err := c.tracker.ReclaimStory(context.Background(), id)
	if err != nil {
		return fmt.Errorf("reclaiming %s: %w", id, err)
	}
	c.lease.reclaimed = reclaimed
	return nil
}

func (c *readyContext) theStoryHoldsALeaseUntil(id, clock string) error {
	if c.lease.beatErr != nil {
		return fmt.Errorf("the heartbeat failed: %w", c.lease.beatErr)
	}
	if c.lease.claimErr != nil {
		return fmt.Errorf("the claim failed: %w", c.lease.claimErr)
	}
	return c.holds(id, apptest.Actor, clock)
}

func (c *readyContext) theStoryIsStillHeldBy(id, holder, clock string) error {
	return c.holds(id, holder, clock)
}

// holds checks a story is in progress, held by holder, with a lease that runs
// out at clock on leaseDay.
func (c *readyContext) holds(id, holder, clock string) error {
	when, err := time.Parse("15:04", clock)
	if err != nil {
		return fmt.Errorf("reading the time %q: %w", clock, err)
	}
	want := leaseDay.Add(time.Duration(when.Hour())*time.Hour + time.Duration(when.Minute())*time.Minute)
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != application.StatusInProgress || detail.Assignee != holder {
		return fmt.Errorf("expected %s in progress and held by %s, got %q held by %q", id, holder, detail.Status, detail.Assignee)
	}
	if !detail.LeaseExpires.Equal(want) {
		return fmt.Errorf("expected the lease on %s to run until %s, got %v", id, want.Format(time.RFC3339), detail.LeaseExpires)
	}
	return nil
}

func (c *readyContext) theStaleClaimsAre(clock, want string) error {
	now, err := c.at(clock)
	if err != nil {
		return err
	}
	stale, err := c.tracker.StaleClaims(context.Background(), now)
	if err != nil {
		return fmt.Errorf("listing the stale claims: %w", err)
	}
	if got := strings.Join(apptest.IDs(stale), ", "); got != want {
		return fmt.Errorf("expected the stale claims at %s to be %q, got %q", clock, want, got)
	}
	return nil
}

func (c *readyContext) theClaimWasReclaimed(id string) error {
	if !c.lease.reclaimed {
		return fmt.Errorf("expected the claim on %s to have been reclaimed, and it was not", id)
	}
	return nil
}

func (c *readyContext) theClaimWasNotReclaimed(id string) error {
	if c.lease.reclaimed {
		return fmt.Errorf("expected the claim on %s to be left alone, and it was reclaimed", id)
	}
	return nil
}

func (c *readyContext) theStoryIsOpenAndHeldByNobody(id string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != application.StatusOpen || detail.Assignee != "" || !detail.LeaseExpires.IsZero() {
		return fmt.Errorf("expected %s open, unassigned and without a lease, got %q held by %q until %v",
			id, detail.Status, detail.Assignee, detail.LeaseExpires)
	}
	return nil
}

func (c *readyContext) theHeartbeatFails() error {
	if c.lease.beatErr == nil {
		return fmt.Errorf("expected the heartbeat to fail, and it went through")
	}
	return nil
}

func (c *readyContext) theClaimFailsBecauseHeld(holder string) error {
	var held *application.ClaimHeldError
	if !errors.As(c.lease.claimErr, &held) {
		return fmt.Errorf("expected the claim to fail because the story is held, got %v", c.lease.claimErr)
	}
	if held.Holder != holder {
		return fmt.Errorf("expected the claim to name %s as the holder, got %s", holder, held.Holder)
	}
	return nil
}

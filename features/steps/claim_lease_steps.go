package steps

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

	// claimedAt, heartbeats and staleSeen are what
	// "mw next heartbeats ... while its session runs" leaves behind: when the
	// story was last claimed, the simulated time of each successful
	// HeartbeatClaim, and whether the story was ever found among the stale
	// claims while it ran.
	claimedAt  time.Time
	heartbeats []time.Time
	staleSeen  bool
}

// heartbeatSpy is a WorkTracker that records the simulated time of every
// successful HeartbeatClaim it makes, into seen, so a scenario can check the
// gaps between them without the fake tracker itself having to know about
// simulated time.
type heartbeatSpy struct {
	*apptest.FakeTracker
	seen *[]time.Time
	now  func() time.Time
}

func (h heartbeatSpy) HeartbeatClaim(ctx context.Context, id string) error {
	if err := h.FakeTracker.HeartbeatClaim(ctx, id); err != nil {
		return err
	}
	*h.seen = append(*h.seen, h.now())
	return nil
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

	ctx.Given(`^the session of "([^"]*)" is running$`, c.theSessionIsRunning)
	ctx.When(`^mw next heartbeats "([^"]*)" while its session runs for (\d+) minutes?$`, c.mwNextHeartbeatsWhileItsSessionRunsForMinutes)
	ctx.Then(`^the story "([^"]*)" was heartbeated at least every (\d+) minutes?$`, c.theStoryWasHeartbeatedAtLeastEveryMinutes)
	ctx.Then(`^the story "([^"]*)" was never among the stale claims while its session ran$`, c.theStoryWasNeverAmongTheStaleClaims)
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
	c.lease.claimedAt = c.lease.now
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

func (c *readyContext) theSessionIsRunning(id string) error {
	return c.runner.Start(context.Background(), application.SessionSpec{
		Name:    application.SessionName(id),
		Command: []string{"true"},
	})
}

// mwNextHeartbeatsWhileItsSessionRunsForMinutes drives application.Next's
// Heartbeat with a clock that advances, on every wait, by exactly the wait it
// was asked for — so a session "running" for minutes never really waits, but
// the fake tracker's clock moves precisely as it would if it did. The
// session's runner session is ended, as if the Builder's harness had just
// exited, once the simulated clock reaches minutes past when the loop
// started waiting; the very next check of the session then stops the loop,
// exactly as a real session ending does.
func (c *readyContext) mwNextHeartbeatsWhileItsSessionRunsForMinutes(id, minutesText string) error {
	minutes, err := strconv.Atoi(minutesText)
	if err != nil {
		return fmt.Errorf("parsing %q as minutes: %w", minutesText, err)
	}
	name := application.SessionName(id)
	ends := c.lease.now.Add(time.Duration(minutes) * time.Minute)
	c.lease.heartbeats = nil
	c.lease.staleSeen = false

	spy := heartbeatSpy{FakeTracker: c.tracker, seen: &c.lease.heartbeats, now: func() time.Time { return c.lease.now }}

	n := application.Next{
		Tracker: spy,
		Runner:  c.runner,
		HeartbeatWait: func(ctx context.Context, d time.Duration) error {
			c.lease.now = c.lease.now.Add(d)
			c.tracker.Clock = func() time.Time { return c.lease.now }
			stale, err := c.tracker.StaleClaims(ctx, c.lease.now)
			if err != nil {
				return err
			}
			for _, s := range stale {
				if s.Story.ID == id {
					c.lease.staleSeen = true
				}
			}
			if !c.lease.now.Before(ends) {
				c.runner.Exit(name, 0)
			}
			return nil
		},
	}
	return n.Heartbeat(context.Background(), id)
}

func (c *readyContext) theStoryWasHeartbeatedAtLeastEveryMinutes(id, minutesText string) error {
	minutes, err := strconv.Atoi(minutesText)
	if err != nil {
		return fmt.Errorf("parsing %q as minutes: %w", minutesText, err)
	}
	interval := time.Duration(minutes) * time.Minute
	if len(c.lease.heartbeats) == 0 {
		return fmt.Errorf("expected %s to have been heartbeated, and it never was", id)
	}
	last := c.lease.claimedAt
	for _, at := range c.lease.heartbeats {
		if at.Sub(last) > interval {
			return fmt.Errorf("expected %s to be heartbeated at least every %s, but %s passed between %s and %s",
				id, interval, at.Sub(last), last.Format("15:04"), at.Format("15:04"))
		}
		last = at
	}
	return nil
}

func (c *readyContext) theStoryWasNeverAmongTheStaleClaims(id string) error {
	if c.lease.staleSeen {
		return fmt.Errorf("expected %s never to be among the stale claims while its session ran, and it was", id)
	}
	return nil
}

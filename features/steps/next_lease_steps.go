package steps

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// The steps of features/next_lease.feature run the close-out of next.feature's
// world with the claim's lease under watch: a clock that moves only when the
// heartbeat waits, and a tracker that counts the renewals.

// registerNextLeaseSteps registers them with nextContext.
func registerNextLeaseSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's tests take a while$`, c.theRigsTestsTakeAWhile)
	ctx.Given(`^every wait for a heartbeat passes (\d+) minutes of the claim's clock$`, c.everyWaitPassesMinutes)

	ctx.Then(`^the claim on "([^"]*)" was heartbeaten while it was landed$`, c.theClaimWasHeartbeatenWhileLanded)
	ctx.Then(`^the claim on "([^"]*)" was never among the stale claims while it was landed$`, c.theClaimWasNeverStaleWhileLanded)
	ctx.Then(`^the claim on "([^"]*)" is not heartbeaten once the close-out has returned$`, c.theClaimIsNotHeartbeatenAfterwards)
}

// landingLease is what a scenario of next_lease.feature watches: the claim's
// clock, the renewals made and the times the story was found stale.
type landingLease struct {
	mu      sync.Mutex
	now     time.Time
	step    time.Duration
	beats   int
	stale   int
	tracker *apptest.FakeTracker
	id      string
}

func (l *landingLease) clock() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.now
}

// wait is Next's HeartbeatWait: a short real pause, then the claim's clock
// moves on by step, and the tracker is asked whether the claim has lapsed.
func (l *landingLease) wait(ctx context.Context, _ time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(20 * time.Millisecond):
	}
	l.mu.Lock()
	l.now = l.now.Add(l.step)
	now := l.now
	l.mu.Unlock()
	stale, err := l.tracker.StaleClaims(ctx, now)
	if err != nil {
		return err
	}
	for _, s := range stale {
		if s.Story.ID == l.id {
			l.mu.Lock()
			l.stale++
			l.mu.Unlock()
		}
	}
	return nil
}

func (l *landingLease) counts() (beats, stale int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.beats, l.stale
}

// watch is the tracker Next is given: the fake, counting every renewal.
func (l *landingLease) watch(t *apptest.FakeTracker) application.WorkTracker {
	return leaseCounter{FakeTracker: t, lease: l}
}

type leaseCounter struct {
	*apptest.FakeTracker
	lease *landingLease
}

func (h leaseCounter) HeartbeatClaim(ctx context.Context, id string) error {
	if err := h.FakeTracker.HeartbeatClaim(ctx, id); err != nil {
		return err
	}
	h.lease.mu.Lock()
	h.lease.beats++
	h.lease.mu.Unlock()
	return nil
}

func (c *nextContext) theRigsTestsTakeAWhile() error {
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; sleep 1", c.checkLog)
	return nil
}

func (c *nextContext) everyWaitPassesMinutes(minutes int) error {
	c.lease = &landingLease{
		now:     time.Now(),
		step:    time.Duration(minutes) * time.Minute,
		tracker: c.tracker,
		id:      "mw-gq6.1",
	}
	// The claim was made in the Background, on the real clock: renew it on the
	// watched one, so that its lease runs from where the watch begins.
	c.tracker.Clock = c.lease.clock
	return c.tracker.HeartbeatClaim(context.Background(), c.lease.id)
}

func (c *nextContext) theClaimWasHeartbeatenWhileLanded(id string) error {
	if beats, _ := c.lease.counts(); beats == 0 {
		return fmt.Errorf("expected the claim on %s to be heartbeaten while it was landed, and it never was", id)
	}
	return nil
}

func (c *nextContext) theClaimWasNeverStaleWhileLanded(id string) error {
	if _, stale := c.lease.counts(); stale > 0 {
		return fmt.Errorf("expected the claim on %s never to be among the stale claims while it was landed, and it was %d times", id, stale)
	}
	return nil
}

func (c *nextContext) theClaimIsNotHeartbeatenAfterwards(id string) error {
	before, _ := c.lease.counts()
	time.Sleep(200 * time.Millisecond)
	if after, _ := c.lease.counts(); after != before {
		return fmt.Errorf("expected no heartbeat of %s once the close-out returned, and %d more were made", id, after-before)
	}
	return nil
}

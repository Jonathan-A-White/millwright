package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// slotsContext is what features/grist_concurrent.feature adds to a mill: more
// grind slots than the first, and grists whose photos differ so that one
// grist's answer does not delete another's.
type slotsContext struct {
	c        *gristContext
	extra    []*apptest.FakeGristLock
	distinct bool
	sent     int
}

// more is the slots after the first, as the mill and dispatch are given them.
func (s *slotsContext) more() []application.GristLock {
	locks := make([]application.GristLock, 0, len(s.extra))
	for _, lock := range s.extra {
		locks = append(locks, lock)
	}
	return locks
}

// photoSalt is what makes this grist's photo its own, when a scenario sends
// several and says so.
func (s *slotsContext) photoSalt() string {
	if !s.distinct {
		return ""
	}
	s.sent++
	return fmt.Sprintf(" of grist %d", s.sent)
}

func registerGristSlots(ctx *godog.ScenarioContext, c *gristContext) {
	s := &c.slots
	// After the reset of gristContext's own Before hook, which clears it.
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		s.c = c
		return ctx, nil
	})

	ctx.Given(`^the mill grinds up to (\d+) grists at once$`, s.slotsAre)
	ctx.Given(`^each grist carries a photo of its own$`, s.photosAreDistinct)
	ctx.Given(`^the grinds are held until (\d+) of them are running at once$`, s.grindsMeet)
	ctx.Given(`^(\d+) grinds? (?:is|are) running on "([^"]*)"$`, s.grindsAreRunning)

	ctx.Then(`^at most (\d+) grinds? ran at once$`, s.mostAtOnce)
	ctx.Then(`^the mill's cursor is past all (\d+) grists$`, s.cursorPastAll)
	ctx.Then(`^the first two runs overlap in their timing$`, s.runsOverlap)
}

func (s *slotsContext) slotsAre(n int) error {
	s.extra = nil
	for i := 1; i < n; i++ {
		s.extra = append(s.extra, &apptest.FakeGristLock{})
	}
	return nil
}

func (s *slotsContext) photosAreDistinct() error {
	s.distinct = true
	return nil
}

func (s *slotsContext) grindsMeet(n int) error {
	s.c.grinder.Together = n
	return nil
}

// grindsAreRunning holds the first n slots, as n grinds of another pass would.
func (s *slotsContext) grindsAreRunning(n int, host string) error {
	if host != s.c.host {
		return fmt.Errorf("the mill is on %s, not %s", s.c.host, host)
	}
	if n < 1 || n > 1+len(s.extra) {
		return fmt.Errorf("the mill has %d slots, so %d grinds cannot be running", 1+len(s.extra), n)
	}
	s.c.grinding.Hold()
	for _, lock := range s.extra[:n-1] {
		lock.Hold()
	}
	return nil
}

func (s *slotsContext) mostAtOnce(n int) error {
	if got := s.c.grinder.MostAtOnce(); got != n {
		return fmt.Errorf("expected at most %d grinds at once, got %d", n, got)
	}
	return nil
}

func (s *slotsContext) cursorPastAll(n int) error {
	records, _ := s.c.backend.Messages(context.Background(), 0)
	if len(records) != n {
		return fmt.Errorf("expected %d grists, the backend holds %d", n, len(records))
	}
	return s.c.theCursorIsPastThem()
}

// runsOverlap reads the two earliest runs mw grist runs listed: the second
// was received before the first had ended.
func (s *slotsContext) runsOverlap() error {
	listed := s.c.audio.listed
	if len(listed) < 2 {
		return fmt.Errorf("expected at least two runs listed, got %d:\n%s", len(listed), s.c.audio.listing.String())
	}
	first, second := listed[0], listed[1]
	ended := first.Received.Add(time.Duration(first.Seconds * float64(time.Second)))
	if !second.Received.Before(ended) {
		return fmt.Errorf("expected the second run received at %s before the first ended at %s:\n%s",
			second.Received.Format("15:04:05"), ended.Format("15:04:05"), s.c.audio.listing.String())
	}
	return nil
}

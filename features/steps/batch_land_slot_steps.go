package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// slotWatch looks at the rig's merge slot as a Builder's `mw check --part N`
// would, at the moments a batch landing waits and at those it runs the rig's
// tests (features/batch_land.feature, mw-4hqh0en): each probe tries to take the
// slot shared for a moment and notes whether it could.
type slotWatch struct {
	c *nextContext
	// started is, for each phase, whether every probe of it could take the slot.
	started map[string]bool
	probed  map[string]int
}

func (w *slotWatch) probe(phase string) {
	slot, ok := w.c.mergeSlot().(application.SharedMergeSlot)
	if !ok {
		return
	}
	taking, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	holding, err := slot.TakeShared(taking, w.c.rig, "builder checking a part of a story")
	if err == nil {
		_ = holding.Release(context.Background())
	}
	if w.started == nil {
		w.started, w.probed = map[string]bool{}, map[string]int{}
	}
	if _, seen := w.started[phase]; !seen {
		w.started[phase] = true
	}
	w.started[phase] = w.started[phase] && err == nil
	w.probed[phase]++
}

// probingChecks is the rig's tests with a probe of the slot as they run.
type probingChecks struct {
	application.Checks
	w *slotWatch
}

func (p probingChecks) Run(ctx context.Context, rig, dir string) (application.Checked, error) {
	p.w.probe("the batch ran the rig's tests")
	return p.Checks.Run(ctx, rig, dir)
}

// probingFixer probes the slot while the batch waits for its fixer.
type probingFixer struct {
	*fakeFixer
	w *slotWatch
}

func (p probingFixer) Wait(ctx context.Context, name string) (application.SessionStatus, error) {
	p.w.probe("the batch waited for its fixer")
	return p.fakeFixer.Wait(ctx, name)
}

func registerBatchLandSlotSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^a check of the rig tries to take the merge slot whenever the batch waits for its fixer or for the host, and when it runs the rig's tests$`, func() error {
		c.batch.slotWatch = &slotWatch{c: c}
		return nil
	})
	ctx.Given(`^the host is busy when the batch first looks, and calms while it waits$`, func() error {
		c.batch.busyThenCalm = true
		return nil
	})
	ctx.Then(`^the check could take the merge slot while the batch waited for ([a-z ]+)$`, func(phase string) error {
		return c.batch.slotWatch.expect("the batch waited for "+phase, true, c)
	})
	ctx.Then(`^the check could not take the merge slot while the batch ran the rig's tests$`, func() error {
		return c.batch.slotWatch.expect("the batch ran the rig's tests", false, c)
	})
}

// expect says whether the probes of phase were made, and all of them could take
// the slot (could) or none could.
func (w *slotWatch) expect(phase string, could bool, c *nextContext) error {
	if w == nil || w.probed[phase] == 0 {
		return fmt.Errorf("expected the slot to be looked at while %s, it never was\n%s", phase, c.said())
	}
	if w.started[phase] != could {
		return fmt.Errorf("expected a check %s take the merge slot while %s\n%s", map[bool]string{true: "to", false: "not to"}[could], phase, c.said())
	}
	return nil
}

// busyThenCalmLoad is a host that is busy for its first reading and calm after.
func busyThenCalmLoad() *apptest.FakeHostLoad {
	return &apptest.FakeHostLoad{Readings: []application.LoadReading{{Load: 39, Cores: 20}, {Load: 3, Cores: 20}}}
}

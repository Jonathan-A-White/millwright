package steps

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of features/next_slot_cap.feature (mw-gq6.467): a close-out whose
// merge slot is still held at the overall most it waits. The waiting itself is
// features/merge_slot.feature's; here the slot is a stand-in that has waited.

// registerNextSlotCapSteps registers the steps of features/next_slot_cap.feature.
func registerNextSlotCapSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the merge slot of the rig is still held at the overall most a close-out waits$`, c.theSlotIsHeldAtTheCap)
	ctx.Then(`^the story "([^"]*)" is recorded as handed back for its close-out alone$`, c.theStoryIsRecordedAsHandedBack)
}

// slotAtTheCap is a merge slot whose every Take has waited the overall most and
// found it still held by a live close-out, as rig.Slots says so.
type slotAtTheCap struct{}

func (slotAtTheCap) Take(_ context.Context, rigDir, _ string) (application.Holding, error) {
	return nil, fmt.Errorf("the merge slot of %s was still held by %q after 2h0m0s, the overall most a close-out waits for it: %w",
		rigDir, "builder@vps closing out mw-gq6.99", application.ErrSlotCap)
}

func (c *nextContext) theSlotIsHeldAtTheCap() error {
	c.slot = slotAtTheCap{}
	return nil
}

func (c *nextContext) theStoryIsRecordedAsHandedBack(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunCloseOutAgain {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q (the close-out said: %v)\n%s",
			id, application.RunState, application.RunCloseOutAgain, got, c.err, c.printed.String())
	}
	if !c.report.HandedBack || c.report.Landed {
		return fmt.Errorf("expected the report to say %s was handed back and not landed, got %+v", id, c.report)
	}
	return nil
}

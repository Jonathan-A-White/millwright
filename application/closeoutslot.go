package application

import (
	"context"
	"fmt"
)

// The slots a close-out of one story holds while it lands: the rig's merge slot
// on this host, taken alone before the merge and given back after the push, and
// inside it the rig's merge slot across hosts.

// mergeYielding is merge with the rig's merge slot on this host, holding, given
// up to shared for the close-out's own waits on the way, as a batch landing gives
// its slot up (SlotYield, mw-4hqh0en): the merged result's wait for a calm host
// and for this host's gate lock is made with the slot shared, so that a whole
// check of the rig, which takes the slot shared, is not held up by it, and the
// slot is taken alone again before the rig's tests run on the merged result
// (mw-gq6.1424). A slot that could not be taken alone again pushes nothing
// (slotLostBeforePush). A holding that cannot be yielded (a fake, or a slot of
// another kind) is kept through the waits, as it always was: a SlotYield that
// gives nothing up would still give the gate lock back after each wait for it.
func (n Next) mergeYielding(ctx context.Context, c *closeOut, report *NextReport, holding Holding) (Landed, error) {
	if _, yields := holding.(YieldingHolding); yields && n.SlotYield == nil {
		c.slot = &batchSlot{holding: holding}
		n.SlotYield, n.SlotShared = c.slot.yield, c.slot.shared
		defer func() {
			report.Notes = append(report.Notes, c.slot.notes...)
			c.slot.notes = nil
		}()
	}
	return n.merge(ctx, c, report)
}

// slotLostBeforePush is why the close-out no longer holds its rig's merge slot
// alone after a wait it gave the slot up for, nil while it does: nothing is
// tested or pushed then. A wait that ran past the overall most on the checks
// sharing the slot (ErrSlotCap) is handed back, as a take of the slot that does.
func (c *closeOut) slotLostBeforePush() error {
	if c.slot == nil || c.slot.lost == nil {
		return nil
	}
	return failedFor(ReasonMergeSlot, fmt.Errorf("the merge slot of %s could not be taken alone again after a wait, so nothing was pushed: %w", c.path.Rig, c.slot.lost))
}

// takeCrossSlot waits for the rig's merge slot across hosts and takes it, and
// the report says what became of it.
func (n Next) takeCrossSlot(ctx context.Context, c *closeOut, report *NextReport) {
	cross, said := n.CrossSlot.Take(ctx, c.path.Rig, Holders(n.Seat, n.Host, c.id))
	report.CrossSlot = append(report.CrossSlot, said...)
	c.cross = cross
}

// waitOffTheSlot gives the rig's merge slot across hosts back while the gate on
// a merged result waits for this host, for waits, and takes it again once it
// is done waiting (mw-gq6.422): the other host lands meanwhile, and the next
// try starts over from a fresh fetch. It is no try lost, since nothing was
// pushed, and it cannot come round for ever: the gate lock, once had, is kept,
// and the host is let calm once before each run. The rig's merge slot on this
// host is shared for the wait when the close-out yields it (mergeYielding); one
// that could not be taken alone again after it ends the landing.
func (n Next) waitOffTheSlot(ctx context.Context, c *closeOut, report *NextReport, waits string) error {
	if c.cross != nil {
		report.CrossSlot = append(report.CrossSlot, fmt.Sprintf("gave the cross-host merge slot of %s back while it waited for %s before %s",
			c.path.Rig, waits, c.merged.what()))
	}
	report.Notes = append(report.Notes, c.cross.Release(ctx)...)
	c.cross = nil
	n.waitForTheMergedGate(ctx, c, report)
	if err := c.slotLostBeforePush(); err != nil {
		return err
	}
	n.takeCrossSlot(ctx, c, report)
	return nil
}

// offTheSlot is a try at a landing that stopped before its gate on the merged
// result, which would have waited for this host, for waits, while holding the
// rig's merge slot across hosts.
type offTheSlot struct{ waits string }

func (o offTheSlot) Error() string {
	return "the rig's tests on the merged result wait for " + o.waits + ", which is not waited for under the merge slot across hosts"
}

package application

import (
	"context"
	"fmt"
)

// batchSlot is the merge slot a batch landing holds, and the waits of its own it
// gives the slot up to the rig's checks for: the fixer's whole run and a busy
// host's calm (mw-4hqh0en). The batch holds the slot alone only while it merges,
// tests and pushes; in a wait it holds it shared, which keeps another landing
// out and lets a Builder's `mw check --part N` start, and before its tests it
// takes the slot alone again, which waits for those checks to end and keeps new
// ones out, so a check never runs beside the batch's own run of the tests. A
// holding that cannot be yielded (a fake, or a slot of another kind) is simply
// kept: the batch waits as it always did.
type batchSlot struct {
	holding Holding
	// yielded says the batch is between a yield and its resume, so that a wait
	// inside a wait gives the slot up once.
	yielded bool
	// lost is why the slot could not be taken alone again after a wait; the
	// tests are not run on the word of a batch that no longer holds it.
	lost error
	// notes are troubles with yielding, for the report.
	notes []string
}

// yield gives the slot up for a wait, and returns the function that takes it
// back alone when the wait is over.
func (s *batchSlot) yield(ctx context.Context) func() {
	nothing := func() {}
	y, ok := s.holding.(YieldingHolding)
	if !ok || s.yielded || s.lost != nil {
		return nothing
	}
	if err := y.Yield(ctx); err != nil {
		s.notes = append(s.notes, fmt.Sprintf("the merge slot could not be shared during a wait, so it was held alone through it: %v", err))
		return nothing
	}
	s.yielded = true
	return func() {
		s.yielded = false
		if err := y.Resume(ctx); err != nil {
			s.lost = err
		}
	}
}

// slotLost is why the batch no longer holds the slot alone, empty while it does.
func (b BatchLand) slotLost() string {
	if b.slot == nil || b.slot.lost == nil {
		return ""
	}
	return fmt.Sprintf("the merge slot could not be taken alone again after a wait: %v", b.slot.lost)
}

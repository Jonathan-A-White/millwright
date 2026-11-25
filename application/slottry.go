package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// TryMergeSlot is a MergeSlot that can also be tried without waiting: a tick that
// would deploy a rig looks at the rig's after-landing slot, and passes the rig over
// when a running deploy holds it, rather than waiting behind that deploy while
// every rig after it waits too (mw-gq6.1605).
type TryMergeSlot interface {
	MergeSlot
	// TryTake takes the rig's slot if it is free this moment and says so with the
	// holding. A slot somebody has is a *SlotHeldError, at once; any other error
	// is a slot that could not be looked at.
	TryTake(ctx context.Context, rigDir, holder string) (Holding, error)
}

// SlotHeldError is what a TryMergeSlot's TryTake returns when somebody has the
// slot: who, and since when when the slot says.
type SlotHeldError struct {
	Path   string
	Holder string
	// Since is when the holder took the slot, zero when the slot does not say.
	Since time.Time
}

func (e *SlotHeldError) Error() string {
	return fmt.Sprintf("the slot %s is held by %q%s", e.Path, e.Holder, e.sinceWords())
}

func (e *SlotHeldError) sinceWords() string {
	if e.Since.IsZero() {
		return ""
	}
	return ", since " + e.Since.UTC().Format("15:04:05Z")
}

// Words is who has the slot and since when, as a tick's note says it.
func (e *SlotHeldError) Words() string { return fmt.Sprintf("%s%s", e.Holder, e.sinceWords()) }

// takenSlot is a slot already taken, handed to a run that takes its slot itself:
// its Take gives the holding once, so that the run gives it back and no second
// take of the same lock waits behind the first.
type takenSlot struct{ holding Holding }

func (t takenSlot) Take(context.Context, string, string) (Holding, error) { return t.holding, nil }

// tryAfterLandingSlot is how a tick takes a rig's after-landing slot: without
// waiting. It returns the slot the run is to take (the one already held, or the
// slot itself when it cannot be tried) and, when a running deploy holds it, who,
// and a release for a hold the run may not have given back.
func tryAfterLandingSlot(ctx context.Context, slot MergeSlot, rigDir, holder string) (use MergeSlot, heldBy *SlotHeldError, release func()) {
	nothing := func() {}
	try, ok := slot.(TryMergeSlot)
	if !ok {
		return slot, nil, nothing
	}
	holding, err := try.TryTake(ctx, rigDir, holder)
	var held *SlotHeldError
	switch {
	case errors.As(err, &held):
		return slot, held, nothing
	case err != nil:
		// A slot that cannot be looked at is left to the run, which says why.
		return slot, nil, nothing
	}
	return takenSlot{holding}, nil, func() { _ = holding.Release(ctx) }
}

// afterLandingHolder is what the rig's after-landing slot says of whoever runs the
// rig's command.
func afterLandingHolder(host, rig string) string {
	return fmt.Sprintf("%s running the after-landing command of %s", SeatIdentity(MwSeat, host), rig)
}

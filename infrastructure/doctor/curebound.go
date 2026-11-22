package doctor

import (
	"context"
	"fmt"
	"time"
)

// A cure runs inside a run that ends (Doctor.step hands it the run's ctx, which
// carries what is left of RunBudget plus StepBudget). So a cure takes that ctx
// as the parent of every call and wait it makes, gives itself one deadline of
// its own on top with cureContext, waits with waitCtx instead of time.Sleep,
// and words a cut with cutError, so that it neither outlasts the run nor sleeps
// through it, and says plainly when it was stopped part-way (mw-gq6.1641).

// cureContext is ctx with one deadline of the cure's own, own from now: the
// earlier of that and the run's end. An own of zero or less adds none.
func cureContext(ctx context.Context, own time.Duration) (context.Context, context.CancelFunc) {
	if own <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, own)
}

// waitCtx waits d, or until ctx ends, whichever is first, returning ctx's error
// when it ended first. A non-nil sleep stands in for the real wait (a test's),
// and is then run whole, with ctx checked once it returns.
func waitCtx(ctx context.Context, d time.Duration, sleep func(time.Duration)) error {
	if sleep != nil {
		sleep(d)
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// cutError is the error of a cure whose ctx ended while it still had work to do:
// what it was doing, what it left done, and that the next run finds the fault
// again and retries it. It keeps ctx's own error, so errors.Is sees the
// deadline. It is nil when ctx has not ended.
func cutError(ctx context.Context, doing, left string) error {
	if ctx.Err() == nil {
		return nil
	}
	return fmt.Errorf("the cure was cut while %s (%w); %s; it will be retried by the next run", doing, ctx.Err(), left)
}

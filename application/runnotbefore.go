package application

import (
	"context"
	"time"
)

type runNotBeforeKey struct{}

// WithRunNotBefore is ctx with the earliest a run begun under it may have begun
// to stand for it (mw-gq6.1639): a nightly run whose state still holds a result
// no follower was told, from a run that began before at, is that night's no longer,
// and is cleared, not told, when the run is begun. A zero at says nothing.
func WithRunNotBefore(ctx context.Context, at time.Time) context.Context {
	if at.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, runNotBeforeKey{}, at)
}

// RunNotBefore is the time WithRunNotBefore put in ctx, and false when none is.
func RunNotBefore(ctx context.Context) (time.Time, bool) {
	at, ok := ctx.Value(runNotBeforeKey{}).(time.Time)
	return at, ok
}

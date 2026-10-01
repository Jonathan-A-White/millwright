package apptest

import (
	"context"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeTracker also feeds the follower.
var _ application.BeadFeed = (*FakeTracker)(nil)

// SetBeadStates sets every bead BeadStates reports and the newest change's
// time it gives with them.
func (f *FakeTracker) SetBeadStates(newest time.Time, beads ...application.BeadNow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beadStates = append([]application.BeadNow(nil), beads...)
	f.beadNewest = newest
}

// AddBeadChange records a change BeadChanges reports from then on.
func (f *FakeTracker) AddBeadChange(c application.BeadChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beadChanges = append(f.beadChanges, c)
}

// BeadStates implements application.BeadFeed.
func (f *FakeTracker) BeadStates(context.Context) ([]application.BeadNow, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]application.BeadNow(nil), f.beadStates...), f.beadNewest, nil
}

// BeadChanges implements application.BeadFeed: the changes added at or after
// since, in the order they were added.
func (f *FakeTracker) BeadChanges(_ context.Context, since time.Time) ([]application.BeadChange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []application.BeadChange
	for _, c := range f.beadChanges {
		if !c.At.Before(since) {
			out = append(out, c)
		}
	}
	return out, nil
}

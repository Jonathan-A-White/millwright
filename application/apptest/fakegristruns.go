package apptest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeGristRuns is an in-memory application.GristRunStore holding only the
// timing of each run, which is all the room check reads: a test seeds it with
// the grinds it wants the mill to have done, or lets a fake grind Keep its own.
type FakeGristRuns struct {
	mu   sync.Mutex
	Kept []application.GristRunTiming
}

// FakeGristRuns satisfies the port.
var _ application.GristRunStore = (*FakeGristRuns)(nil)

// Keep implements application.GristRunStore.
func (f *FakeGristRuns) Keep(_ context.Context, run application.GristRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Kept = append(f.Kept, run.Timing)
	return nil
}

// List implements application.GristRunStore.
func (f *FakeGristRuns) List(_ context.Context, since time.Time) ([]application.GristRunLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var lines []application.GristRunLine
	for _, t := range f.Kept {
		if since.IsZero() || !t.Received.Before(since) {
			lines = append(lines, application.GristRunLine{Txid: t.Txid, Kind: t.Kind, Model: t.Model, Received: t.Received, Seconds: t.Seconds})
		}
	}
	return lines, nil
}

// Timings implements application.GristRunStore, oldest first.
func (f *FakeGristRuns) Timings(_ context.Context) ([]application.GristRunTiming, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]application.GristRunTiming(nil), f.Kept...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Received.Before(out[j].Received) })
	return out, nil
}

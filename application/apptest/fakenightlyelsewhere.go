package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeNightlyElsewhere is an application.NightlyElsewhere with a host book a test
// writes: the rooms the other hosts say they have, and the nightly runs begun on
// them, in order, as "host kind rig".
type FakeNightlyElsewhere struct {
	mu sync.Mutex
	// Hosts are the rooms every other host says, whatever the rig; Err is why the
	// book cannot be read.
	Hosts []application.BatchRoom
	Err   error
	// RunErr is what a run on a host ends with.
	RunErr error
	// OnRun, when set, runs for each run begun on a host, before it ends.
	OnRun func(host, kind, rig string)

	ran   []string
	since []time.Time
}

var _ application.NightlyElsewhere = (*FakeNightlyElsewhere)(nil)

// Rooms implements application.NightlyElsewhere.
func (f *FakeNightlyElsewhere) Rooms(context.Context, string) ([]application.BatchRoom, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.BatchRoom(nil), f.Hosts...), nil
}

// Run implements application.NightlyElsewhere.
func (f *FakeNightlyElsewhere) Run(ctx context.Context, host, kind, rig string) error {
	f.mu.Lock()
	f.ran = append(f.ran, host+" "+kind+" "+rig)
	at, _ := application.RunNotBefore(ctx)
	f.since = append(f.since, at)
	on, err := f.OnRun, f.RunErr
	f.mu.Unlock()
	if on != nil {
		on(host, kind, rig)
	}
	return err
}

// Ran is the runs begun on other hosts, as "host kind rig".
func (f *FakeNightlyElsewhere) Ran() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

// NotBefore is, for each run begun on another host (in the order of Ran), the
// earliest begin the run was told to take a leftover result for its own
// (application.RunNotBefore); the zero time when it was told none.
func (f *FakeNightlyElsewhere) NotBefore() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.since...)
}

package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBatchElsewhere is an application.BatchElsewhere with a host book a test
// writes: the rooms the other hosts say they have, and the landings begun on
// them, in order, as "host rig".
type FakeBatchElsewhere struct {
	mu sync.Mutex
	// Hosts are the rooms every other host says, whatever the rig; Err is why the
	// book cannot be read.
	Hosts []application.BatchRoom
	Err   error
	// LandErr is what a landing on a host ends with.
	LandErr error

	asked  []string
	landed []string
}

var _ application.BatchElsewhere = (*FakeBatchElsewhere)(nil)

// Rooms implements application.BatchElsewhere.
func (f *FakeBatchElsewhere) Rooms(_ context.Context, rig string) ([]application.BatchRoom, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, rig)
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.BatchRoom(nil), f.Hosts...), nil
}

// Land implements application.BatchElsewhere.
func (f *FakeBatchElsewhere) Land(_ context.Context, host, rig string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landed = append(f.landed, host+" "+rig)
	return f.LandErr
}

// Asked is the rigs the host book was asked about, in order.
func (f *FakeBatchElsewhere) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// Landed is the landings begun on another host, as "host rig".
func (f *FakeBatchElsewhere) Landed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.landed...)
}

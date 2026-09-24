package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeReach satisfies application.Reach: reachable until a test says
// otherwise.
var _ application.Reach = (*FakeReach)(nil)

// FakeReach answers Reachable with Ok, and counts how often it was asked.
type FakeReach struct {
	mu sync.Mutex

	Ok    bool
	asked int
}

// NewFakeReach is a Reach that starts out reachable.
func NewFakeReach() *FakeReach {
	return &FakeReach{Ok: true}
}

// Reachable implements application.Reach.
func (f *FakeReach) Reachable(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	return f.Ok
}

// SetReachable changes what Reachable answers.
func (f *FakeReach) SetReachable(ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ok = ok
}

// Asked is how many times Reachable was called.
func (f *FakeReach) Asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

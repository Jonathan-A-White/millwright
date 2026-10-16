package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeHeartbeats is an in-memory application.LiveHeartbeats: the heartbeat
// processes alive, by story.
type FakeHeartbeats struct {
	mu    sync.Mutex
	alive map[string][]int

	// Err, when set, is returned instead of looking.
	Err error
}

// FakeHeartbeats satisfies the port.
var _ application.LiveHeartbeats = (*FakeHeartbeats)(nil)

// NewFakeHeartbeats is a process table holding no heartbeat.
func NewFakeHeartbeats() *FakeHeartbeats {
	return &FakeHeartbeats{alive: map[string][]int{}}
}

// Start puts a heartbeat of storyID alive, as process pid.
func (f *FakeHeartbeats) Start(storyID string, pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[storyID] = append(f.alive[storyID], pid)
}

// Stop ends every heartbeat of storyID.
func (f *FakeHeartbeats) Stop(storyID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.alive, storyID)
}

// Beating implements application.LiveHeartbeats.
func (f *FakeHeartbeats) Beating(_ context.Context, storyID string) ([]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]int(nil), f.alive[storyID]...), nil
}

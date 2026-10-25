package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeWorktreeRemote is an in-memory application.WorktreeRemote: a test says
// what each host's worktree of a story holds, or that a host cannot be reached,
// and reads back which hosts were asked. A host it was told nothing of cannot
// be reached.
type FakeWorktreeRemote struct {
	mu      sync.Mutex
	states  map[string]application.WorktreeState
	refuses map[string]error
	asked   []string
}

// NewFakeWorktreeRemote returns a remote that reaches no host.
func NewFakeWorktreeRemote() *FakeWorktreeRemote {
	return &FakeWorktreeRemote{states: map[string]application.WorktreeState{}, refuses: map[string]error{}}
}

var _ application.WorktreeRemote = (*FakeWorktreeRemote)(nil)

// Holds makes host answer with state.
func (f *FakeWorktreeRemote) Holds(host string, state application.WorktreeState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[host] = state
	delete(f.refuses, host)
}

// Unreachable makes asking host fail with err.
func (f *FakeWorktreeRemote) Unreachable(host string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuses[host] = err
}

// Asked reports the hosts asked, oldest first.
func (f *FakeWorktreeRemote) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// WorktreeState implements application.WorktreeRemote.
func (f *FakeWorktreeRemote) WorktreeState(_ context.Context, host, _ string) (application.WorktreeState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, host)
	if err, ok := f.refuses[host]; ok {
		return application.WorktreeState{}, err
	}
	if state, ok := f.states[host]; ok {
		return state, nil
	}
	return application.WorktreeState{}, fmt.Errorf("no way to reach %s", host)
}

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

	retryOut   map[string]string
	retryFails map[string]error
	retried    []string
	retryFlags []string
}

// NewFakeWorktreeRemote returns a remote that reaches no host.
func NewFakeWorktreeRemote() *FakeWorktreeRemote {
	return &FakeWorktreeRemote{states: map[string]application.WorktreeState{}, refuses: map[string]error{},
		retryOut: map[string]string{}, retryFails: map[string]error{}}
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

// RetriesWith makes asking host to retry a story succeed, printing out.
func (f *FakeWorktreeRemote) RetriesWith(host, out string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryOut[host] = out
	delete(f.retryFails, host)
}

// RetryFails makes asking host to retry a story fail with err.
func (f *FakeWorktreeRemote) RetryFails(host string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryFails[host] = err
}

// Retried reports the retries asked for, each "<host> <story>", oldest first.
func (f *FakeWorktreeRemote) Retried() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.retried...)
}

// RetryFlags reports the flags of the last retry asked for.
func (f *FakeWorktreeRemote) RetryFlags() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.retryFlags...)
}

// RetryThere implements application.WorktreeRemote. A host it was told nothing
// of cannot be asked to retry.
func (f *FakeWorktreeRemote) RetryThere(_ context.Context, host, storyID string, flags []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retried = append(f.retried, host+" "+storyID)
	f.retryFlags = append([]string(nil), flags...)
	if err, ok := f.retryFails[host]; ok {
		return "", err
	}
	if out, ok := f.retryOut[host]; ok {
		return out, nil
	}
	return "", fmt.Errorf("no way to reach %s", host)
}

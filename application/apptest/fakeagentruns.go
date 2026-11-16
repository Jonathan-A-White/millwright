package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeAgentRunMarks is an application.AgentRunMarks held in memory: a scenario
// puts a mark in Gone to say a process left it behind.
type FakeAgentRunMarks struct {
	mu   sync.Mutex
	live map[string]application.AgentRun
	// Gone are the marks left by a process that is not there.
	Gone []application.AgentRun
	// OpenErr is what Open fails with.
	OpenErr error
}

var _ application.AgentRunMarks = (*FakeAgentRunMarks)(nil)

// Begin implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) Begin(_ context.Context, run application.AgentRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.live == nil {
		f.live = map[string]application.AgentRun{}
	}
	f.live[run.Job] = run
	return nil
}

// End implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) End(_ context.Context, run application.AgentRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.live, run.Job)
	return nil
}

// Open implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) Open(context.Context) (live, gone []application.AgentRun, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.OpenErr != nil {
		return nil, nil, f.OpenErr
	}
	for _, r := range f.live {
		live = append(live, r)
	}
	return live, append([]application.AgentRun(nil), f.Gone...), nil
}

// Forget implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) Forget(_ context.Context, run application.AgentRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.Gone[:0:0]
	for _, g := range f.Gone {
		if g.Job != run.Job {
			kept = append(kept, g)
		}
	}
	f.Gone = kept
	return nil
}

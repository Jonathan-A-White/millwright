package apptest

import (
	"context"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeAgentRunMarks is an application.AgentRunMarks held in memory: a scenario
// puts a mark in Gone to say a process left it behind.
type FakeAgentRunMarks struct {
	mu   sync.Mutex
	live map[string]application.AgentRun
	held map[string]time.Time
	// Gone are the marks left by a process that is not there.
	Gone []application.AgentRun
	// OpenErr is what Open fails with.
	OpenErr error
	// HeldErr is what Held fails with.
	HeldErr error
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

// Hold implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) Hold(_ context.Context, who string, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.held == nil {
		f.held = map[string]time.Time{}
	}
	f.held[who] = until
	return nil
}

// Release implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) Release(_ context.Context, who string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, who)
	return nil
}

// Held implements application.AgentRunMarks.
func (f *FakeAgentRunMarks) Held(_ context.Context, now time.Time) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.HeldErr != nil {
		return nil, f.HeldErr
	}
	var who []string
	for w, until := range f.held {
		if now.Before(until) {
			who = append(who, w)
		}
	}
	return who, nil
}

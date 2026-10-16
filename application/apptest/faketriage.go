package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeTriage is an in-memory application.TriageSource and TriageCloser: the
// beads a test sets, and the closes asked of it.
type FakeTriage struct {
	mu     sync.Mutex
	Beads  []application.TriageBead
	Err    error
	closed []string
}

// TriageBeads implements application.TriageSource.
func (f *FakeTriage) TriageBeads(context.Context) ([]application.TriageBead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]application.TriageBead(nil), f.Beads...), nil
}

// CloseStory implements application.TriageCloser.
func (f *FakeTriage) CloseStory(_ context.Context, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
	return nil
}

// Closed is the ids asked to be closed, in order.
func (f *FakeTriage) Closed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closed...)
}

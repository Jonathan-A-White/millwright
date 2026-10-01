package apptest

import (
	"context"
	"sort"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// FakePrompts is an in-memory application.Prompts: the saved prompts, by name.
type FakePrompts struct {
	mu      sync.Mutex
	prompts map[string]domain.Prompt
	puts    []domain.Prompt

	// Err, when set, is returned by every method instead of doing the work.
	Err error
}

// FakePrompts satisfies the port.
var _ application.Prompts = (*FakePrompts)(nil)

// NewFakePrompts returns a backend with no prompt saved.
func NewFakePrompts() *FakePrompts {
	return &FakePrompts{prompts: map[string]domain.Prompt{}}
}

// List reports every prompt, by name.
func (f *FakePrompts) List(context.Context) ([]domain.Prompt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	names := make([]string, 0, len(f.prompts))
	for name := range f.prompts {
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]domain.Prompt, 0, len(names))
	for _, name := range names {
		list = append(list, f.prompts[name])
	}
	return list, nil
}

// Get reports the prompt saved under name.
func (f *FakePrompts) Get(_ context.Context, name string) (domain.Prompt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return domain.Prompt{}, false, f.Err
	}
	p, ok := f.prompts[name]
	return p, ok, nil
}

// Put saves p under its name, and records the call.
func (f *FakePrompts) Put(_ context.Context, p domain.Prompt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.prompts[p.Name] = p
	f.puts = append(f.puts, p)
	return nil
}

// Delete removes the prompt saved under name.
func (f *FakePrompts) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	delete(f.prompts, name)
	return nil
}

// Puts reports every prompt Put was asked to save, in order.
func (f *FakePrompts) Puts() []domain.Prompt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Prompt(nil), f.puts...)
}

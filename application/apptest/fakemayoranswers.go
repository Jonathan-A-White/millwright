package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeMayorAnswers is an in-memory application.MayorAnswers: a test says what
// each host prints when mw next is run there, or that it fails, and reads back
// which hosts were asked and with which flags. A host it was told nothing of
// cannot be reached.
type FakeMayorAnswers struct {
	mu    sync.Mutex
	out   map[string]string
	fails map[string]error
	calls []MayorAnswerCall
}

// MayorAnswerCall is one run of mw next on a host.
type MayorAnswerCall struct {
	Host, Story string
	Flags       []string
}

// NewFakeMayorAnswers returns a remote that reaches no host.
func NewFakeMayorAnswers() *FakeMayorAnswers {
	return &FakeMayorAnswers{out: map[string]string{}, fails: map[string]error{}}
}

var _ application.MayorAnswers = (*FakeMayorAnswers)(nil)

// Prints makes mw next on host succeed and print out.
func (f *FakeMayorAnswers) Prints(host, out string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out[host] = out
	delete(f.fails, host)
}

// Fails makes mw next on host fail with err.
func (f *FakeMayorAnswers) Fails(host string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fails[host] = err
}

// Calls is every run asked of a host, oldest first.
func (f *FakeMayorAnswers) Calls() []MayorAnswerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]MayorAnswerCall(nil), f.calls...)
}

// NextThere implements application.MayorAnswers.
func (f *FakeMayorAnswers) NextThere(_ context.Context, host, storyID string, flags []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, MayorAnswerCall{Host: host, Story: storyID, Flags: append([]string(nil), flags...)})
	if err, ok := f.fails[host]; ok {
		return f.out[host], err
	}
	if out, ok := f.out[host]; ok {
		return out, nil
	}
	return "", fmt.Errorf("no way to reach %s", host)
}

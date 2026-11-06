package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBdTurn is an in-memory application.BdTurn: one turn, taken by whoever asks
// first, with a mutex behind it so that a test of two bd commands at once means
// it.
type FakeBdTurn struct {
	mu     sync.Mutex
	held   bool
	Holder string
	Err    error
}

// FakeBdTurn satisfies the port.
var _ application.BdTurn = (*FakeBdTurn)(nil)

// NewFakeBdTurn is a turn nobody holds.
func NewFakeBdTurn() *FakeBdTurn { return &FakeBdTurn{Holder: "another mw bd"} }

// Hold takes the turn as someone else, who keeps it until the release is run.
func (f *FakeBdTurn) Hold() func() {
	f.mu.Lock()
	f.held = true
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.held = false
		f.mu.Unlock()
	}
}

// TryTake implements application.BdTurn.
func (f *FakeBdTurn) TryTake(context.Context) (func(), bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, false, "", f.Err
	}
	if f.held {
		return nil, false, f.Holder, nil
	}
	f.held = true
	return func() {
		f.mu.Lock()
		f.held = false
		f.mu.Unlock()
	}, true, "", nil
}

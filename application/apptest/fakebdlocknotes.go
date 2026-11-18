package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeBdLockNotes is an in-memory application.BdLockNotes.
type FakeBdLockNotes struct {
	mu    sync.Mutex
	notes map[string]string
	// ReadErr, when set, is what Lost fails with.
	ReadErr error
}

// FakeBdLockNotes satisfies the port.
var _ application.BdLockNotes = (*FakeBdLockNotes)(nil)

// Lose implements application.BdLockNotes.
func (f *FakeBdLockNotes) Lose(_ context.Context, ids []string, blocker string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notes == nil {
		f.notes = map[string]string{}
	}
	for _, id := range ids {
		f.notes[id] = blocker
	}
	return nil
}

// Lost implements application.BdLockNotes.
func (f *FakeBdLockNotes) Lost(_ context.Context, ids []string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReadErr != nil {
		return nil, f.ReadErr
	}
	got := map[string]string{}
	for _, id := range ids {
		if blocker, ok := f.notes[id]; ok {
			got[id] = blocker
		}
	}
	return got, nil
}

// Forget implements application.BdLockNotes.
func (f *FakeBdLockNotes) Forget(_ context.Context, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		delete(f.notes, id)
	}
	return nil
}

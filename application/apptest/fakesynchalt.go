package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeSyncHaltMarker is an in-memory application.SyncHaltMarker: one mark, or
// none.
type FakeSyncHaltMarker struct {
	mu   sync.Mutex
	info application.SyncHaltInfo
	held bool

	// WriteErr, ReadErr and ClearErr, when set, are returned instead of doing
	// the work.
	WriteErr, ReadErr, ClearErr error
}

// FakeSyncHaltMarker satisfies the port.
var _ application.SyncHaltMarker = (*FakeSyncHaltMarker)(nil)

// NewFakeSyncHaltMarker is a marker holding nothing.
func NewFakeSyncHaltMarker() *FakeSyncHaltMarker { return &FakeSyncHaltMarker{} }

// Write implements application.SyncHaltMarker.
func (f *FakeSyncHaltMarker) Write(_ context.Context, info application.SyncHaltInfo) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.info, f.held = info, true
	return nil
}

// Read implements application.SyncHaltMarker.
func (f *FakeSyncHaltMarker) Read(_ context.Context) (application.SyncHaltInfo, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ReadErr != nil {
		return application.SyncHaltInfo{}, false, f.ReadErr
	}
	return f.info, f.held, nil
}

// Clear implements application.SyncHaltMarker.
func (f *FakeSyncHaltMarker) Clear(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ClearErr != nil {
		return f.ClearErr
	}
	f.info, f.held = application.SyncHaltInfo{}, false
	return nil
}

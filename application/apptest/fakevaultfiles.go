package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeVaultFiles is an in-memory application.VaultFiles. No git runs: a test
// says what the clone would have found and the fake remembers what was asked of
// it, so that a use case can be walked through a sync without a remote. The
// feature that covers syncing uses the real adapter on real temp clones; this
// is for the use case's own tests.
type FakeVaultFiles struct {
	mu sync.Mutex

	// Dirty is what Uncommitted reports.
	Dirty []string
	// Marked is whether the vault still had to be told that ledgers merge by
	// union; MarkLedgers reports it and then leaves it false, as a second call
	// on a marked vault would.
	Marked bool
	// Incoming and Outgoing are the commits Pull and Push report moving.
	Incoming, Outgoing int

	// MarkErr, DirtyErr, PullErr and PushErr each stop that step.
	MarkErr, DirtyErr, PullErr, PushErr error

	marks, pulls, pushes int
}

// FakeVaultFiles satisfies the port.
var _ application.VaultFiles = (*FakeVaultFiles)(nil)

// MarkLedgers implements application.VaultFiles.
func (f *FakeVaultFiles) MarkLedgers(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MarkErr != nil {
		return false, f.MarkErr
	}
	f.marks++
	marked := f.Marked
	f.Marked = false
	return marked, nil
}

// Uncommitted implements application.VaultFiles.
func (f *FakeVaultFiles) Uncommitted(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DirtyErr != nil {
		return nil, f.DirtyErr
	}
	return append([]string(nil), f.Dirty...), nil
}

// Pull implements application.VaultFiles.
func (f *FakeVaultFiles) Pull(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.PullErr != nil {
		return 0, f.PullErr
	}
	f.pulls++
	return f.Incoming, nil
}

// Push implements application.VaultFiles.
func (f *FakeVaultFiles) Push(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.PushErr != nil {
		return 0, f.PushErr
	}
	f.pushes++
	return f.Outgoing, nil
}

// Moves reports how often the vault was marked, pulled and pushed, so that a
// test can say a sync stopped before it moved anything.
func (f *FakeVaultFiles) Moves() (marks, pulls, pushes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.marks, f.pulls, f.pushes
}

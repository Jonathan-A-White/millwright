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

	// Unchanged names paths Commit reports as holding nothing to commit, the
	// way the real clone reports a file nobody touched.
	Unchanged []string

	// PullErrFor, when positive, is how many Pulls report PullErr before the
	// next ones work, the way a network that comes back does. Zero leaves
	// PullErr as it always was: every Pull reports it.
	PullErrFor int

	// MarkErr, DirtyErr, PullErr, PushErr and CommitErr each stop that step.
	MarkErr, DirtyErr, PullErr, PushErr, CommitErr error

	marks, pulls, pushes int
	pullTries            int
	commits              []VaultCommit
}

// VaultCommit is one commit the fake was asked to make: what it was asked to
// record, and under what message. A test reads them back with Commits.
type VaultCommit struct {
	Message string
	Paths   []string
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
	f.pullTries++
	if f.PullErr != nil {
		if f.PullErrFor == 0 || f.pullTries <= f.PullErrFor {
			return 0, f.PullErr
		}
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

// Commit implements application.VaultFiles: it writes nothing down but what it
// was asked for, and reports every path asked of it as committed but the ones
// the test said were unchanged.
func (f *FakeVaultFiles) Commit(_ context.Context, message string, paths []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.CommitErr != nil {
		return nil, f.CommitErr
	}

	var recorded []string
	for _, path := range paths {
		if !holds(f.Unchanged, path) {
			recorded = append(recorded, path)
		}
	}
	if len(recorded) == 0 {
		return nil, nil
	}
	f.commits = append(f.commits, VaultCommit{Message: message, Paths: append([]string(nil), paths...)})
	return recorded, nil
}

// Commits is every commit the fake was asked to make, in order.
func (f *FakeVaultFiles) Commits() []VaultCommit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]VaultCommit(nil), f.commits...)
}

// holds reports whether a list names a path.
func holds(list []string, path string) bool {
	for _, held := range list {
		if held == path {
			return true
		}
	}
	return false
}

// Moves reports how often the vault was marked, pulled and pushed, so that a
// test can say a sync stopped before it moved anything.
func (f *FakeVaultFiles) Moves() (marks, pulls, pushes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.marks, f.pulls, f.pushes
}

// PullTries reports how many times Pull was asked, whether it worked or not, so
// that a test can say a failed sync was tried again, or that it was not.
func (f *FakeVaultFiles) PullTries() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pullTries
}

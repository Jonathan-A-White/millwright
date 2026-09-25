package apptest

import (
	"context"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeSnapshotFile is an in-memory application.PosternSnapshotFile: it keeps
// only the last data written, so a test can decrypt it back and compare.
type FakeSnapshotFile struct {
	mu sync.Mutex

	path   string
	data   []byte
	writes int

	// Err, when set, is returned by Write instead of keeping anything.
	Err error
}

// FakeSnapshotFile satisfies the port.
var _ application.PosternSnapshotFile = (*FakeSnapshotFile)(nil)

// NewFakeSnapshotFile returns a fake snapshot file reporting path.
func NewFakeSnapshotFile(path string) *FakeSnapshotFile {
	return &FakeSnapshotFile{path: path}
}

// Path implements application.PosternSnapshotFile.
func (f *FakeSnapshotFile) Path() string { return f.path }

// Write implements application.PosternSnapshotFile.
func (f *FakeSnapshotFile) Write(_ context.Context, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.data = append([]byte(nil), data...)
	f.writes++
	return nil
}

// Written is the bytes the last successful Write held.
func (f *FakeSnapshotFile) Written() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.data...)
}

// Writes reports how many times Write succeeded.
func (f *FakeSnapshotFile) Writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

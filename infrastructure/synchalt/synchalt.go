// Package synchalt is the mark a host leaves itself when its own sync halts:
// a file on that host, replaced whole so a write never leaves half of one, and
// read straight back by mw status here — never through the tracker, which is
// exactly what a halted sync cannot carry.
package synchalt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// File is the mark's name inside its directory.
const File = "sync-halted"

// Marker is the sync-halted mark kept in Dir, made when first written.
type Marker struct {
	Dir string
}

// Marker satisfies the port.
var _ application.SyncHaltMarker = (*Marker)(nil)

// New is the mark kept in dir.
func New(dir string) *Marker { return &Marker{Dir: dir} }

func (m *Marker) path() string { return filepath.Join(m.Dir, File) }

// Write implements application.SyncHaltMarker. The file is replaced whole, by
// renaming a new one over it, so that a write killed half way leaves the old
// mark, or none, and never half of a new one.
func (m *Marker) Write(_ context.Context, info application.SyncHaltInfo) error {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", m.Dir, err)
	}
	path := m.path()
	next := path + ".new"
	if err := os.WriteFile(next, []byte(application.FormatSyncHalt(info)), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", next, err)
	}
	if err := os.Rename(next, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Read implements application.SyncHaltMarker. A mark that cannot be parsed
// reads as none, rather than a failure.
func (m *Marker) Read(_ context.Context) (application.SyncHaltInfo, bool, error) {
	held, err := os.ReadFile(m.path())
	switch {
	case os.IsNotExist(err):
		return application.SyncHaltInfo{}, false, nil
	case err != nil:
		return application.SyncHaltInfo{}, false, fmt.Errorf("reading %s: %w", m.path(), err)
	}
	info, ok := application.ParseSyncHalt(string(held))
	return info, ok, nil
}

// Clear implements application.SyncHaltMarker.
func (m *Marker) Clear(_ context.Context) error {
	if err := os.Remove(m.path()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", m.path(), err)
	}
	return nil
}

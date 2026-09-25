package postern

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.PosternSnapshotFile = (*SnapshotFile)(nil)

// SnapshotFile is where mw postern snapshot writes the encrypted snapshot, at
// path: config postern_snapshot_path.
type SnapshotFile struct {
	path string
}

// NewSnapshotFile is the snapshot file at path.
func NewSnapshotFile(path string) *SnapshotFile {
	return &SnapshotFile{path: path}
}

// Path implements application.PosternSnapshotFile.
func (f *SnapshotFile) Path() string { return f.path }

// Write implements application.PosternSnapshotFile: a temp file beside path,
// written whole, then renamed over it, so nginx or a reader never sees a
// half-written one.
func (f *SnapshotFile) Write(_ context.Context, data []byte) error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	temp := f.path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", temp, err)
	}
	if err := os.Rename(temp, f.path); err != nil {
		return fmt.Errorf("moving %s into place: %w", temp, err)
	}
	return nil
}

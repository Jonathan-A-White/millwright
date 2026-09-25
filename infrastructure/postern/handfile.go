package postern

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// HandFile is the real application.PosternHandFile: mw postern serve and mw
// postern nginx's file edits, straight against the filesystem.
type HandFile struct {
	// Now stamps a backup's filename. The zero value reads the real clock.
	Now func() time.Time
}

var _ application.PosternHandFile = (*HandFile)(nil)

// NewHandFile returns a HandFile reading the real clock.
func NewHandFile() *HandFile { return &HandFile{} }

// Read implements application.PosternHandFile.
func (h *HandFile) Read(_ context.Context, path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), true, nil
}

// Backup implements application.PosternHandFile: path's current content,
// copied into dir under its own base name and a UTC timestamp, so two runs
// in the same second never collide by more than that timestamp's precision.
func (h *HandFile) Backup(_ context.Context, path, dir string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s to back it up: %w", path, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("making the backup directory %s: %w", dir, err)
	}
	backupPath := filepath.Join(dir, fmt.Sprintf("%s.%s.bak", filepath.Base(path), h.now().UTC().Format("20060102T150405Z")))
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return "", fmt.Errorf("writing the backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}

// Write implements application.PosternHandFile.
func (h *HandFile) Write(_ context.Context, path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making the directory of %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// MkdirAll implements application.PosternHandFile.
func (h *HandFile) MkdirAll(_ context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	return nil
}

func (h *HandFile) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

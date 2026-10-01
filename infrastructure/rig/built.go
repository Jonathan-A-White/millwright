package rig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// BuiltMarks is the commit a host last built for each rig, kept as one file a
// rig in Dir, made when first written. It is the adapter behind
// application.BuiltMarks.
type BuiltMarks struct {
	Dir string
}

// BuiltMarks satisfies the port.
var _ application.BuiltMarks = (*BuiltMarks)(nil)

// NewBuiltMarks is the marks kept in dir.
func NewBuiltMarks(dir string) *BuiltMarks { return &BuiltMarks{Dir: dir} }

func (b *BuiltMarks) path(rig string) string { return filepath.Join(b.Dir, "built-"+rig) }

// Built implements application.BuiltMarks.
func (b *BuiltMarks) Built(_ context.Context, rig string) (string, error) {
	held, err := os.ReadFile(b.path(rig))
	switch {
	case os.IsNotExist(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("reading %s: %w", b.path(rig), err)
	}
	return strings.TrimSpace(string(held)), nil
}

// MarkBuilt implements application.BuiltMarks. The file is replaced whole, by
// renaming a new one over it.
func (b *BuiltMarks) MarkBuilt(_ context.Context, rig, commit string) error {
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", b.Dir, err)
	}
	next := b.path(rig) + ".new"
	if err := os.WriteFile(next, []byte(commit+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", next, err)
	}
	if err := os.Rename(next, b.path(rig)); err != nil {
		return fmt.Errorf("replacing %s: %w", b.path(rig), err)
	}
	return nil
}

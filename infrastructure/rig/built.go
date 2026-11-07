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
// renaming a new one over it. The new file is named for this call alone, so
// two writers at once never share it; the later rename wins, whole.
func (b *BuiltMarks) MarkBuilt(_ context.Context, rig, commit string) error {
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", b.Dir, err)
	}
	f, err := os.CreateTemp(b.Dir, "built-"+rig+".*.new")
	if err != nil {
		return fmt.Errorf("making a file beside %s: %w", b.path(rig), err)
	}
	next := f.Name()
	fail := func(format string, err error) error {
		_ = f.Close()
		_ = os.Remove(next)
		return fmt.Errorf(format, next, err)
	}
	if _, err := f.WriteString(commit + "\n"); err != nil {
		return fail("writing %s: %w", err)
	}
	if err := f.Chmod(0o644); err != nil {
		return fail("setting the mode of %s: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("closing %s: %w", next, err)
	}
	if err := os.Rename(next, b.path(rig)); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("replacing %s: %w", b.path(rig), err)
	}
	return nil
}

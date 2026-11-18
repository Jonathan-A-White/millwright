package hostlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// BdLockDir is the name of the directory, inside the state directory, that
// holds one file for each formula step whose close a mw bd gave up on for the bd
// lock (mw-gq6.1505): the file is named by the step's id and holds who the close
// waited for.
const BdLockDir = "bd-lock-lost"

// BdLockNotes is application.BdLockNotes kept as files in Dir/BdLockDir.
type BdLockNotes struct{ Dir string }

// BdLockNotes satisfies the port.
var _ application.BdLockNotes = BdLockNotes{}

// NewBdLockNotes is the notes kept under dir.
func NewBdLockNotes(dir string) BdLockNotes { return BdLockNotes{Dir: dir} }

// path is where the note of id is kept; "" for an id that is no plain bead id.
func (n BdLockNotes) path(id string) string {
	if id == "" || strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return ""
	}
	return filepath.Join(n.Dir, BdLockDir, id)
}

// Lose implements application.BdLockNotes.
func (n BdLockNotes) Lose(_ context.Context, ids []string, blocker string) error {
	if err := os.MkdirAll(filepath.Join(n.Dir, BdLockDir), 0o700); err != nil {
		return fmt.Errorf("making the directory the bd lock notes belong in: %w", err)
	}
	for _, id := range ids {
		if path := n.path(id); path != "" {
			if err := os.WriteFile(path, []byte(strings.TrimSpace(blocker)+"\n"), 0o600); err != nil {
				return fmt.Errorf("writing the bd lock note of %s: %w", id, err)
			}
		}
	}
	return nil
}

// Lost implements application.BdLockNotes.
func (n BdLockNotes) Lost(_ context.Context, ids []string) (map[string]string, error) {
	got := map[string]string{}
	for _, id := range ids {
		path := n.path(id)
		if path == "" {
			continue
		}
		said, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			return nil, fmt.Errorf("reading the bd lock note of %s: %w", id, err)
		default:
			got[id] = strings.TrimSpace(string(said))
		}
	}
	return got, nil
}

// Forget implements application.BdLockNotes.
func (n BdLockNotes) Forget(_ context.Context, ids []string) error {
	for _, id := range ids {
		if path := n.path(id); path != "" {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("removing the bd lock note of %s: %w", id, err)
			}
		}
	}
	return nil
}

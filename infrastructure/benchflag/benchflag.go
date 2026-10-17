// Package benchflag is the memory of the calibration flags a landing report
// has printed on this host: one small file per kind in a directory of this
// host's state, holding when the flag was printed (RFC 3339) and its text.
package benchflag

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Dir is the directory of flag files inside the state directory.
const Dir = "bench-flags"

// Memory is the flags printed, kept in Dir.
type Memory struct{ Dir string }

var _ application.BenchFlagMemory = (*Memory)(nil)

// New is the memory kept in dir.
func New(dir string) *Memory { return &Memory{Dir: dir} }

// path is the kind's file; a kind has slashes in it, so its name is escaped.
func (m *Memory) path(kind string) string { return filepath.Join(m.Dir, url.PathEscape(kind)) }

// Said implements application.BenchFlagMemory. A file that cannot be parsed
// reads as none.
func (m *Memory) Said(_ context.Context, kind string) (string, time.Time, bool, error) {
	held, err := os.ReadFile(m.path(kind))
	if os.IsNotExist(err) {
		return "", time.Time{}, false, nil
	} else if err != nil {
		return "", time.Time{}, false, fmt.Errorf("reading %s: %w", m.path(kind), err)
	}
	stamp, text, found := strings.Cut(strings.TrimSuffix(string(held), "\n"), "\n")
	at, perr := time.Parse(time.RFC3339, stamp)
	if !found || perr != nil {
		return "", time.Time{}, false, nil
	}
	return text, at, true, nil
}

// Say implements application.BenchFlagMemory. The file is replaced whole, by
// renaming a new one over it.
func (m *Memory) Say(_ context.Context, kind, text string, at time.Time) error {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", m.Dir, err)
	}
	path := m.path(kind)
	next := path + ".new"
	if err := os.WriteFile(next, []byte(at.UTC().Format(time.RFC3339)+"\n"+text+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", next, err)
	}
	if err := os.Rename(next, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

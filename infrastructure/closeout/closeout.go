// Package closeout is the mark a running close-out leaves on its host: one file
// per story in a directory of this host's state, naming the process that wrote
// it. A mark whose process has gone — a close-out killed before it could clear
// its own — reads as no mark, so that a dead close-out never keeps a story from
// the quiet alarm.
package closeout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Dir is the directory of marks inside the state directory.
const Dir = "closing-out"

// Marks are the close-out marks kept in Dir, made when first written.
type Marks struct {
	Dir string
	// Proc is the process table a mark's process is looked for in; empty is /proc.
	Proc string
	// Pid is the process that writes marks; zero is this one.
	Pid int
}

// Marks satisfies the port.
var _ application.CloseOutMarks = (*Marks)(nil)

// New is the marks kept in dir.
func New(dir string) *Marks { return &Marks{Dir: dir} }

func (m *Marks) path(story string) string { return filepath.Join(m.Dir, story) }

func (m *Marks) pid() int {
	if m.Pid != 0 {
		return m.Pid
	}
	return os.Getpid()
}

func (m *Marks) proc() string {
	if m.Proc != "" {
		return m.Proc
	}
	return "/proc"
}

// Write implements application.CloseOutMarks. The file is replaced whole, by
// renaming a new one over it. It holds the writer's pid, when the close-out
// began (RFC 3339) and whether it waits for calm, one per line.
func (m *Marks) Write(_ context.Context, mark application.CloseOutMark) error {
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", m.Dir, err)
	}
	body := fmt.Sprintf("%d\n%s\n%t\n", m.pid(), mark.Since.UTC().Format(time.RFC3339), mark.Calming)
	path := m.path(mark.Story)
	next := path + ".new"
	if err := os.WriteFile(next, []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", next, err)
	}
	if err := os.Rename(next, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Read implements application.CloseOutMarks. A mark that cannot be parsed, or
// whose process is gone, reads as none; the latter is removed.
func (m *Marks) Read(_ context.Context) ([]application.CloseOutMark, error) {
	entries, err := os.ReadDir(m.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("reading %s: %w", m.Dir, err)
	}
	var marks []application.CloseOutMark
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".new") {
			continue
		}
		held, err := os.ReadFile(m.path(e.Name()))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(held)), "\n")
		if len(lines) != 3 {
			continue
		}
		pid, perr := strconv.Atoi(lines[0])
		since, terr := time.Parse(time.RFC3339, lines[1])
		if perr != nil || terr != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(m.proc(), strconv.Itoa(pid))); err != nil {
			_ = os.Remove(m.path(e.Name()))
			continue
		}
		marks = append(marks, application.CloseOutMark{Story: e.Name(), Since: since, Calming: lines[2] == "true"})
	}
	return marks, nil
}

// Clear implements application.CloseOutMarks.
func (m *Marks) Clear(_ context.Context, story string) error {
	if err := os.Remove(m.path(story)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", m.path(story), err)
	}
	return nil
}

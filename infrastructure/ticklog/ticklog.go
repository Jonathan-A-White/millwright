// Package ticklog is the log a host keeps of what its Millhand ticks found: a
// file on that host, one line a tick, cut to its last lines each time.
package ticklog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// File is the log's name inside its directory.
const File = "log"

// Log is the tick log in Dir, made when first written. It keeps the last Keep
// lines; a Keep of zero or less reads application.TickLogLines.
type Log struct {
	Dir  string
	Keep int
}

// Log satisfies the port.
var _ application.TickLog = (*Log)(nil)

// New is the tick log kept in dir.
func New(dir string) *Log { return &Log{Dir: dir} }

// Append implements application.TickLog: the line goes on the end, and only the
// last Keep lines stay. The file is replaced whole, by renaming a new one over
// it, so that a tick killed half way leaves the old log and not half of a new
// one.
func (l *Log) Append(_ context.Context, line string) error {
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", l.Dir, err)
	}
	path := filepath.Join(l.Dir, File)

	var lines []string
	switch held, err := os.ReadFile(path); {
	case err == nil:
		lines = strings.Split(strings.TrimSuffix(string(held), "\n"), "\n")
	case !os.IsNotExist(err):
		return fmt.Errorf("reading %s: %w", path, err)
	}
	lines = append(lines, strings.TrimRight(line, "\r\n"))
	if keep := l.keep(); len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}

	next := path + ".new"
	if err := os.WriteFile(next, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", next, err)
	}
	if err := os.Rename(next, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Read implements application.TickLog: the lines the log holds, oldest first. A
// log never written to holds none. Nothing is made or changed.
func (l *Log) Read(_ context.Context) ([]string, error) {
	path := filepath.Join(l.Dir, File)
	held, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	text := strings.TrimSuffix(string(held), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func (l *Log) keep() int {
	if l.Keep <= 0 {
		return application.TickLogLines
	}
	return l.Keep
}

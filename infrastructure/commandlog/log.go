// Package commandlog keeps the host's record of mw's own runs: one JSON line a
// run, appended to commands.jsonl under ~/.local/state/mw. It is host state and
// never the vault, and it holds the command's path, exit status and duration,
// never its arguments.
package commandlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.CommandLog = (*Log)(nil)

// File is the log's name under the state directory.
const File = "commands.jsonl"

// Log is the command log at one path.
type Log struct{ path string }

// New is the log at path, which need not exist yet.
func New(path string) *Log { return &Log{path: path} }

// Default is the log under the user's ~/.local/state/mw.
func Default() (*Log, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(home, ".local", "state", "mw", File)), nil
}

// Path reports where the log is kept.
func (l *Log) Path() string { return l.path }

// AppendCommandRun implements application.CommandLog: one line, appended.
func (l *Log) AppendCommandRun(_ context.Context, run application.CommandRun) error {
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("making the directory of %s: %w", l.path, err)
	}
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", l.path, err)
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", l.path, err)
	}
	return file.Close()
}

// CommandRunsSince implements application.CommandLog: the lines that read, at
// or after the time, oldest first. No log is no runs.
func (l *Log) CommandRunsSince(_ context.Context, since time.Time) ([]application.CommandRun, error) {
	file, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", l.path, err)
	}
	defer file.Close()
	var runs []application.CommandRun
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var run application.CommandRun
		if json.Unmarshal(scan.Bytes(), &run) == nil && !run.At.Before(since) {
			runs = append(runs, run)
		}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", l.path, err)
	}
	return runs, nil
}

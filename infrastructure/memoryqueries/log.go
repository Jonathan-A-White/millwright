// Package memoryqueries keeps the host's log of `mw memory query` runs: one
// JSON line a query, appended to a file under ~/.local/state/mw. It is host
// state and never the vault, so a Builder's query leaves the vault's clone
// clean.
package memoryqueries

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.MemoryQueryLog = (*Log)(nil)

// File is the log's name under the state directory.
const File = "memory-queries.jsonl"

// Log is the query log at one path.
type Log struct{ path string }

// New is the log at path, which need not exist yet.
func New(path string) *Log { return &Log{path: path} }

// Path reports where the log is kept.
func (l *Log) Path() string { return l.path }

// AppendMemoryQuery implements application.MemoryQueryLog: one line, appended.
func (l *Log) AppendMemoryQuery(_ context.Context, record application.MemoryQueryRecord) error {
	raw, err := json.Marshal(record)
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

// ReadMemoryQueries implements application.MemoryQueryLog: every line that
// reads, oldest first; no log is no queries, and a line that does not read is
// left out.
func (l *Log) ReadMemoryQueries(_ context.Context) ([]application.MemoryQueryRecord, error) {
	file, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", l.path, err)
	}
	defer file.Close()
	var records []application.MemoryQueryRecord
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scan.Scan() {
		var record application.MemoryQueryRecord
		if json.Unmarshal(scan.Bytes(), &record) == nil {
			records = append(records, record)
		}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", l.path, err)
	}
	return records, nil
}

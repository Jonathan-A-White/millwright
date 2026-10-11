// Package reportlog keeps the days mw report send has gone out: a file on this
// host, one JSON line a send, so the report is sent at most once a UTC day.
package reportlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// File is the list's name inside its directory.
const File = "sent.jsonl"

// DefaultDir is the directory the list is kept in, under the home directory:
// beside the cards', in a directory of its own.
var DefaultDir = filepath.Join(".local", "state", "mw", "report")

// Log is the list kept in Dir, made when first written.
type Log struct{ Dir string }

var _ application.ReportSentLog = (*Log)(nil)

// New is the list kept in dir.
func New(dir string) *Log { return &Log{Dir: dir} }

// Record implements application.ReportSentLog: one line on the end of the file.
func (l *Log) Record(_ context.Context, sent application.ReportSent) error {
	line, err := json.Marshal(sent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", l.Dir, err)
	}
	path := filepath.Join(l.Dir, File)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return f.Close()
}

// Sent implements application.ReportSentLog: the sends, oldest first. A line
// that is not a send is skipped rather than hiding the rest.
func (l *Log) Sent(context.Context) ([]application.ReportSent, error) {
	path := filepath.Join(l.Dir, File)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()
	var sent []application.ReportSent
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lines.Scan() {
		var one application.ReportSent
		if json.Unmarshal(lines.Bytes(), &one) == nil && one.Day != "" {
			sent = append(sent, one)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return sent, nil
}

// Package cardlog keeps the Mayor's list of the cards it has sent: a file on
// this host, one JSON line a card or update, so a card's txid can be found
// after the one time it was printed.
package cardlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// File is the list's name inside its directory.
const File = "cards.jsonl"

// DefaultDir is the directory the list is kept in, under the home directory.
var DefaultDir = filepath.Join(".local", "state", "mw")

// Log is the list kept in Dir, made when first written.
type Log struct{ Dir string }

var _ application.CardLog = (*Log)(nil)

// New is the list kept in dir.
func New(dir string) *Log { return &Log{Dir: dir} }

// Append implements application.CardLog: one line on the end of the file.
func (l *Log) Append(_ context.Context, record domain.CardRecord) error {
	line, err := json.Marshal(record)
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

// List implements application.CardLog: the records, oldest first. A line that
// is not a record is skipped rather than hiding the rest.
func (l *Log) List(context.Context) ([]domain.CardRecord, error) {
	path := filepath.Join(l.Dir, File)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()
	var records []domain.CardRecord
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lines.Scan() {
		var record domain.CardRecord
		if json.Unmarshal(lines.Bytes(), &record) == nil && record.Txid != "" {
			records = append(records, record)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return records, nil
}

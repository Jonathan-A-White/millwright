// Package grist keeps the mill's own state on its host (application's
// GristState): a cursor, the append-only record of every grist handled,
// grinds.jsonl, and any answer the postern backend would not yet take. All of
// it lives in one private directory, config grist_state_dir, and none of it
// in beads: grist never waits on the beads lock.
package grist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// The files inside the state directory.
const (
	CursorFile     = "cursor"
	RecordFile     = "grinds.jsonl"
	UndeliveredDir = "undelivered"
)

// State is the mill's state, kept in Dir.
type State struct {
	Dir string
}

// State satisfies the port.
var _ application.GristState = (*State)(nil)

// New is the mill's state kept in dir.
func New(dir string) *State { return &State{Dir: dir} }

// Cursor implements application.GristState.
func (s *State) Cursor(context.Context) (int64, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, CursorFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading the mill's cursor: %w", err)
	}
	said := strings.TrimSpace(string(raw))
	if said == "" {
		return 0, nil
	}
	seq, err := strconv.ParseInt(said, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the mill's cursor %s says %q, not a whole number: %w", filepath.Join(s.Dir, CursorFile), said, err)
	}
	return seq, nil
}

// SetCursor implements application.GristState: written whole, then renamed
// into place.
func (s *State) SetCursor(_ context.Context, seq int64) error {
	return s.writeWhole(filepath.Join(s.Dir, CursorFile), []byte(strconv.FormatInt(seq, 10)+"\n"))
}

// Lines implements application.GristState. A line that is not one of the
// record's is an error, never skipped: skipping it could answer its grist
// twice.
func (s *State) Lines(context.Context) ([]application.GrindLine, error) {
	path := filepath.Join(s.Dir, RecordFile)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the mill's record: %w", err)
	}
	defer file.Close()
	var lines []application.GrindLine
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; scanner.Scan(); n++ {
		text := bytes.TrimSpace(scanner.Bytes())
		if len(text) == 0 {
			continue
		}
		var line application.GrindLine
		if err := json.Unmarshal(text, &line); err != nil {
			return nil, fmt.Errorf("line %d of the mill's record %s is not one of its lines: %w", n, path, err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading the mill's record: %w", err)
	}
	return lines, nil
}

// Append implements application.GristState: one line, written in one go to
// the end of the record, 0600.
func (s *State) Append(_ context.Context, line application.GrindLine) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("making the mill's state directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.Dir, RecordFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening the mill's record: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("appending to the mill's record: %w", err)
	}
	return file.Close()
}

// KeepUndelivered implements application.GristState: one file per answer,
// named by its grist's txid.
func (s *State) KeepUndelivered(_ context.Context, answer application.GristUndelivered) error {
	encoded, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	return s.writeWhole(s.undeliveredPath(answer.Txid), encoded)
}

// Undelivered implements application.GristState, in the order of their
// file names.
func (s *State) Undelivered(context.Context) ([]application.GristUndelivered, error) {
	dir := filepath.Join(s.Dir, UndeliveredDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the answers kept for delivery: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var kept []application.GristUndelivered
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading the answer kept as %s: %w", e.Name(), err)
		}
		var u application.GristUndelivered
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, fmt.Errorf("the answer kept as %s is not one: %w", e.Name(), err)
		}
		kept = append(kept, u)
	}
	return kept, nil
}

// Delivered implements application.GristState. An answer not kept is
// already forgotten.
func (s *State) Delivered(_ context.Context, txid string) error {
	err := os.Remove(s.undeliveredPath(txid))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// undeliveredPath is where the answer to txid is kept: its colon made a
// dash, and nothing in it able to climb out of the directory.
func (s *State) undeliveredPath(txid string) string {
	name := strings.NewReplacer(":", "-", "/", "-", `\`, "-").Replace(txid)
	return filepath.Join(s.Dir, UndeliveredDir, name+".json")
}

// writeWhole writes data to path, 0600, through a temporary file renamed
// into place, making the directory 0700 first.
func (s *State) writeWhole(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return os.Rename(tmp.Name(), path)
}

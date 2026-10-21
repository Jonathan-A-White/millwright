// Package ideasonly keeps mw's two host files of the Governor's Ideas only:
// state.json, what mw last knew of the mode, and held.jsonl, one line for each
// record of his that mw is keeping until the mode ends. They live together
// under ~/.local/state/mw/postern/ideas-only, host state and never the vault.
package ideasonly

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.IdeasOnlyFiles = (*Files)(nil)

// Files is the state file and the held file in dir.
type Files struct {
	dir string
}

// New is the files in dir, which need not exist yet.
func New(dir string) *Files { return &Files{dir: dir} }

func (f *Files) statePath() string { return filepath.Join(f.dir, "state.json") }
func (f *Files) heldPath() string  { return filepath.Join(f.dir, "held.jsonl") }

// State implements application.IdeasOnlyFiles: the zero state when the file is
// not there; a file that does not read is an error naming it.
func (f *Files) State(_ context.Context) (application.IdeasOnlyState, error) {
	raw, err := os.ReadFile(f.statePath())
	if os.IsNotExist(err) {
		return application.IdeasOnlyState{}, nil
	}
	if err != nil {
		return application.IdeasOnlyState{}, fmt.Errorf("reading %s: %w", f.statePath(), err)
	}
	var state application.IdeasOnlyState
	if err := json.Unmarshal(raw, &state); err != nil {
		return application.IdeasOnlyState{}, fmt.Errorf("%s is not the state of Ideas only: %w", f.statePath(), err)
	}
	return state, nil
}

// SetState implements application.IdeasOnlyFiles: written beside itself and
// renamed over, so a reader never sees half a file.
func (f *Files) SetState(_ context.Context, state application.IdeasOnlyState) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", f.dir, err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", f.statePath(), err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", f.statePath(), err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.statePath())
}

// Held implements application.IdeasOnlyFiles. A line that is not JSON is
// skipped, as ThreadFile skips one.
func (f *Files) Held(_ context.Context) ([]application.IdeasOnlyHeld, error) {
	file, err := os.Open(f.heldPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", f.heldPath(), err)
	}
	defer file.Close()
	var held []application.IdeasOnlyHeld
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var line application.IdeasOnlyHeld
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Txid != "" {
			held = append(held, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.heldPath(), err)
	}
	return held, nil
}

// Released reads the ideas given back, oldest release first: every line of
// released/*.jsonl. A file that cannot be read is passed over: the sleep pass
// reads what there is.
func (f *Files) Released(_ context.Context) ([]application.IdeasOnlyHeld, error) {
	dir := filepath.Join(f.dir, "released")
	names, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	var released []application.IdeasOnlyHeld
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			var idea application.IdeasOnlyHeld
			if json.Unmarshal([]byte(line), &idea) == nil && idea.Txid != "" {
				released = append(released, idea)
			}
		}
	}
	return released, nil
}

// Hold implements application.IdeasOnlyFiles: one line appended, 0600, unless
// the txid is already in the file.
func (f *Files) Hold(ctx context.Context, held application.IdeasOnlyHeld) error {
	have, err := f.Held(ctx)
	if err != nil {
		return err
	}
	for _, line := range have {
		if line.Txid == held.Txid {
			return nil
		}
	}
	raw, err := json.Marshal(held)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", f.dir, err)
	}
	file, err := os.OpenFile(f.heldPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", f.heldPath(), err)
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("writing %s: %w", f.heldPath(), err)
	}
	return file.Close()
}

// Release implements application.IdeasOnlyFiles: held.jsonl is renamed to
// released/<ended, UTC>.jsonl, so the ideas are kept and nothing is held. A
// name already taken gets a counter, so that two modes ending in the same
// second lose nothing.
func (f *Files) Release(_ context.Context, ended time.Time) error {
	if _, err := os.Stat(f.heldPath()); os.IsNotExist(err) {
		return nil
	}
	dir := filepath.Join(f.dir, "released")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	name := ended.UTC().Format("20060102T150405Z")
	target := filepath.Join(dir, name+".jsonl")
	for n := 2; ; n++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", name, n))
	}
	if err := os.Rename(f.heldPath(), target); err != nil {
		return fmt.Errorf("moving %s to %s: %w", f.heldPath(), target, err)
	}
	return nil
}

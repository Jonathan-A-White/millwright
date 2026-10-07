package grist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// RunsDir is the directory of the state directory holding every grind's raw
// record, one directory for each, named by its grist's txid.
const RunsDir = "runs"

// The files of one run's directory; the attachments are attachment-N.<ext>.
const (
	RunInputFile   = "input.json"
	RunScorersFile = "scorers.json"
	RunAnswerFile  = "answer.json"
	RunTimingFile  = "timing.json"
)

// Runs keeps every grind's raw record under Dir/runs/<txid>/ (application's
// GristRunStore). Nothing it writes is ever deleted by mw. It holds the
// grist's photos and recordings as they were opened, so it is private: 0700
// directories, 0600 files.
type Runs struct {
	Dir string
}

// Runs satisfies the port.
var _ application.GristRunStore = (*Runs)(nil)

// NewRuns is the record of runs kept under the state directory dir.
func NewRuns(dir string) *Runs { return &Runs{Dir: dir} }

// Keep implements application.GristRunStore.
func (r *Runs) Keep(_ context.Context, run application.GristRun) error {
	dir := filepath.Join(r.Dir, RunsDir, application.GristRunName(run.Txid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	input := []byte(run.Input)
	if len(input) == 0 {
		input = []byte("null")
	}
	scorers := run.Scorers
	if scorers == nil {
		scorers = []application.GristRunScore{}
	}
	files := []struct {
		name string
		data any
	}{
		{RunInputFile, json.RawMessage(input)},
		{RunScorersFile, scorers},
		{RunAnswerFile, run.Answer},
		{RunTimingFile, run.Timing},
	}
	for _, f := range files {
		encoded, err := json.MarshalIndent(f.data, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), append(encoded, '\n'), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", filepath.Join(dir, f.name), err)
		}
	}
	for i, a := range run.Attachments {
		name := fmt.Sprintf("attachment-%d%s", i+1, a.Ext)
		if err := os.WriteFile(filepath.Join(dir, name), a.Data, 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", filepath.Join(dir, name), err)
		}
	}
	return nil
}

// List implements application.GristRunStore, oldest first. A directory that
// is not a run (no timing.json) is skipped; one whose files do not read is an
// error.
func (r *Runs) List(_ context.Context, since time.Time) ([]application.GristRunLine, error) {
	root := filepath.Join(r.Dir, RunsDir)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the mill's runs: %w", err)
	}
	var lines []application.GristRunLine
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var timing application.GristRunTiming
		if err := readJSON(filepath.Join(root, e.Name(), RunTimingFile), &timing); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if !since.IsZero() && timing.Received.Before(since) {
			continue
		}
		var answer application.GristRunAnswer
		if err := readJSON(filepath.Join(root, e.Name(), RunAnswerFile), &answer); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		lines = append(lines, application.GristRunLine{
			Txid: timing.Txid, Kind: timing.Kind, Model: timing.Model,
			Received: timing.Received, Seconds: timing.Seconds, Status: answer.Status,
		})
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Received.Before(lines[j].Received) })
	return lines, nil
}

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return err
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%s is not what the mill wrote: %w", path, err)
	}
	return nil
}

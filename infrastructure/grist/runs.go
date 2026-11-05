package grist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// RunSenderFile holds the key that sent a forwarded grist, so mw grist
	// request can answer it; only a forward run has one.
	RunSenderFile = "sender.json"
)

// runSender is what RunSenderFile holds.
type runSender struct {
	Key string `json:"key"`
}

// Runs keeps every grind's raw record under Dir/runs/<txid>/ (application's
// GristRunStore). mw grist tidy deletes a run's photos and recordings once
// GristKeepAge has passed; the rest of a run is never deleted. It holds the
// grist's photos and recordings as they were opened, so it is private: 0700
// directories, 0600 files.
type Runs struct {
	Dir string
}

// Runs satisfies the ports.
var (
	_ application.GristRunStore = (*Runs)(nil)
	_ application.GristRunMedia = (*Runs)(nil)
)

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
	if run.Sender != "" {
		encoded, err := json.MarshalIndent(runSender{Key: run.Sender}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, RunSenderFile), append(encoded, '\n'), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", filepath.Join(dir, RunSenderFile), err)
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

// Timings implements application.GristRunStore, oldest first. A directory that
// is not a run (no timing.json) is skipped; a timing.json that does not read is
// an error.
func (r *Runs) Timings(_ context.Context) ([]application.GristRunTiming, error) {
	root := filepath.Join(r.Dir, RunsDir)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the mill's runs: %w", err)
	}
	var timings []application.GristRunTiming
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
		timings = append(timings, timing)
	}
	sort.SliceStable(timings, func(i, j int) bool { return timings[i].Received.Before(timings[j].Received) })
	return timings, nil
}

// Forwarded implements application.GristRunStore: the run of txid when it is
// a forward that kept its sender's key.
func (r *Runs) Forwarded(_ context.Context, txid string) (application.GristForwarded, bool, error) {
	dir := filepath.Join(r.Dir, RunsDir, application.GristRunName(txid))
	var timing application.GristRunTiming
	if err := readJSON(filepath.Join(dir, RunTimingFile), &timing); errors.Is(err, os.ErrNotExist) {
		return application.GristForwarded{}, false, nil
	} else if err != nil {
		return application.GristForwarded{}, false, err
	}
	if timing.Kind != application.GristForwardKind || timing.Txid != txid {
		return application.GristForwarded{}, false, nil
	}
	var sender runSender
	if err := readJSON(filepath.Join(dir, RunSenderFile), &sender); errors.Is(err, os.ErrNotExist) {
		return application.GristForwarded{}, false, nil
	} else if err != nil {
		return application.GristForwarded{}, false, err
	}
	if sender.Key == "" {
		return application.GristForwarded{}, false, nil
	}
	return application.GristForwarded{Txid: txid, App: timing.App, Sender: sender.Key}, true, nil
}

// attachmentPrefix starts the name of every attachment file of a run.
const attachmentPrefix = "attachment-"

// RunMedia implements application.GristRunMedia: the runs that still hold an
// attachment-N.<ext>, oldest first, by the time the mill received the grist. A
// directory with no timing.json is not a run and is skipped; a timing.json
// that does not read is an error.
func (r *Runs) RunMedia(_ context.Context) ([]application.GristMedia, error) {
	root := filepath.Join(r.Dir, RunsDir)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the mill's runs: %w", err)
	}
	var media []application.GristMedia
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Join(root, e.Name()), err)
		}
		held := application.GristMedia{Name: e.Name()}
		for _, f := range files {
			if f.IsDir() || !strings.HasPrefix(f.Name(), attachmentPrefix) {
				continue
			}
			info, err := f.Info()
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", filepath.Join(root, e.Name(), f.Name()), err)
			}
			held.Files++
			held.Bytes += info.Size()
		}
		if held.Files == 0 {
			continue
		}
		var timing application.GristRunTiming
		if err := readJSON(filepath.Join(root, e.Name(), RunTimingFile), &timing); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		held.Txid, held.Received = timing.Txid, timing.Received
		if held.Txid == "" {
			held.Txid = e.Name()
		}
		media = append(media, held)
	}
	sort.SliceStable(media, func(i, j int) bool { return media[i].Received.Before(media[j].Received) })
	return media, nil
}

// DropMedia implements application.GristRunMedia: it removes the run's
// attachment-N.<ext> files and nothing else. A name that is not one directory
// of runs/ is refused.
func (r *Runs) DropMedia(_ context.Context, name string) error {
	if !singleSegment(name) {
		return fmt.Errorf("%q is not the name of a run", name)
	}
	dir := filepath.Join(r.Dir, RunsDir, name)
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}
	var faults []error
	for _, f := range files {
		if f.IsDir() || !strings.HasPrefix(f.Name(), attachmentPrefix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, f.Name())); err != nil {
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

// singleSegment reports whether name is one path segment that climbs nowhere.
func singleSegment(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
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

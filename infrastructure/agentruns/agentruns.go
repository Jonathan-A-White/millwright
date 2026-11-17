// Package agentruns keeps the marks of the follower's model runs, a file for
// each job in a directory of the host's own state, so that the restart a
// landing asks for (another process) can see that a run is under way and wait
// for it (mw-gq6.1339).
package agentruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Marks is application.AgentRunMarks in the directory Dir.
type Marks struct {
	Dir string
	// Pid is the process a Begin marks when the run names none; nil is this one.
	Pid func() int
	// Alive says whether a process is there; nil asks the host with signal 0.
	Alive func(pid int) bool
}

var _ application.AgentRunMarks = Marks{}

// New is the marks kept in dir.
func New(dir string) Marks { return Marks{Dir: dir} }

func (m Marks) path(job string) (string, error) {
	if job == "" || strings.ContainsAny(job, `/\`) || strings.HasPrefix(job, ".") {
		return "", fmt.Errorf("%q is not a job name a mark can be kept under", job)
	}
	return filepath.Join(m.Dir, job+".json"), nil
}

// Begin implements application.AgentRunMarks. The mark is written whole and moved
// into place, so a reader never sees half of one.
func (m Marks) Begin(_ context.Context, run application.AgentRun) error {
	path, err := m.path(run.Job)
	if err != nil {
		return err
	}
	if run.Pid == 0 {
		run.Pid = os.Getpid()
		if m.Pid != nil {
			run.Pid = m.Pid()
		}
	}
	body, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", m.Dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("moving %s into place: %w", tmp, err)
	}
	return nil
}

// End implements application.AgentRunMarks.
func (m Marks) End(_ context.Context, run application.AgentRun) error { return m.remove(run.Job) }

// Forget implements application.AgentRunMarks.
func (m Marks) Forget(_ context.Context, run application.AgentRun) error { return m.remove(run.Job) }

func (m Marks) remove(job string) error {
	path, err := m.path(job)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Open implements application.AgentRunMarks. A mark that cannot be read as one
// is a run that left nothing to wait for and is left alone.
func (m Marks) Open(context.Context) (live, gone []application.AgentRun, err error) {
	entries, err := os.ReadDir(m.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("listing %s: %w", m.Dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(m.Dir, e.Name()))
		if err != nil {
			continue // ended between the listing and the read
		}
		var run application.AgentRun
		if json.Unmarshal(raw, &run) != nil || run.Job == "" || run.Pid <= 0 {
			continue
		}
		if m.alive(run.Pid) {
			live = append(live, run)
		} else {
			gone = append(gone, run)
		}
	}
	return live, gone, nil
}

func (m Marks) alive(pid int) bool {
	if m.Alive != nil {
		return m.Alive(pid)
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// holdPath is where a hold is kept, a file for each who, apart from the
// marks of the runs so that Open does not read one as a run.
func (m Marks) holdPath(who string) (string, error) {
	if who == "" || strings.ContainsAny(who, `/\`) || strings.HasPrefix(who, ".") {
		return "", fmt.Errorf("%q is not a name a hold can be kept under", who)
	}
	return filepath.Join(m.Dir, "holds", who+".hold"), nil
}

// Hold implements application.AgentRunMarks: a file holding the time the hold
// ends, written whole and moved into place.
func (m Marks) Hold(_ context.Context, who string, until time.Time) error {
	path, err := m.holdPath(who)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(until.UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("moving %s into place: %w", tmp, err)
	}
	return nil
}

// Release implements application.AgentRunMarks.
func (m Marks) Release(_ context.Context, who string) error {
	path, err := m.holdPath(who)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Held implements application.AgentRunMarks. A hold that cannot be read as an end
// holds nothing.
func (m Marks) Held(_ context.Context, now time.Time) ([]string, error) {
	dir := filepath.Join(m.Dir, "holds")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", dir, err)
	}
	var who []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".hold")
		if e.IsDir() || !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		until, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
		if err == nil && now.Before(until) {
			who = append(who, name)
		}
	}
	return who, nil
}

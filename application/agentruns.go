package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// A model run of the follower is one of its jobs that starts an agent as its
// own child (the sleep pass, the To do ranking, the mill's grist pass). A
// restart of the follower on a new build ends the process group, so it ends
// the agent too and the run's fuel is spent for nothing (mw-gq6.1339: the
// Sunday sleep pass of 2026-11-15 was cut at 12m30s by a landing). The follower
// therefore marks each such run while it is under way (AgentRunMarks), and the
// restart a landing asks for waits for the marks to be gone, for at most
// AgentRunWaitLimit.

// AgentRunWaitLimit is how long the restart of the follower waits for the model
// runs under way to end: more than the longest Sunday sleep pass seen (12m30s)
// by a good margin, and short enough that the follower is on the new build
// within the half hour. When it passes, the restart goes ahead, says which run
// it cut, and the follower writes that run down (CutRunDir). It is set here and
// nowhere else.
const AgentRunWaitLimit = 30 * time.Minute

// AgentRunPoll is how often the restart looks at the marks while it waits.
const AgentRunPoll = 5 * time.Second

// CutRunDir is the directory under runs/ a cut model run is written in, a file
// for each run: mw fuel says each one.
const CutRunDir = "cut"

// AgentRun is the mark of one model run under way: the job, the host, the
// process of the follower that runs it and when it began.
type AgentRun struct {
	Job   string    `json:"job"`
	Host  string    `json:"host"`
	Pid   int       `json:"pid"`
	Began time.Time `json:"began"`
}

// AgentRunMarks is where the follower's model runs are marked, apart from the
// follower's own process so that a restart (another process) can read it.
type AgentRunMarks interface {
	// Begin marks run as under way, a mark for each job.
	Begin(ctx context.Context, run AgentRun) error
	// End takes run's mark away.
	End(ctx context.Context, run AgentRun) error
	// Open lists the marks: live those whose process still runs, gone those
	// left by a process that is not there (a crash, a host restart).
	Open(ctx context.Context) (live, gone []AgentRun, err error)
	// Forget takes the mark of a gone run away once it is written down.
	Forget(ctx context.Context, run AgentRun) error
}

// CutRun is a model run that did not end: the job, when it began, how long it
// ran, and why it is said to be cut.
type CutRun struct {
	Job   string        `json:"job"`
	Host  string        `json:"host"`
	Began time.Time     `json:"began"`
	Ran   time.Duration `json:"ran_ns"`
	// RanFor is Ran as a person reads it.
	RanFor string `json:"ran"`
	Why    string `json:"why"`
}

// Line is the one line the Mayor reads: the job, when it began, how long it ran.
func (c CutRun) Line() string {
	return fmt.Sprintf("%s began %s, ran %s: %s", c.Job, c.Began.UTC().Format("2006-01-02 15:04:05Z"), c.RanFor, c.Why)
}

// CutRunFiles is where a cut run is written: runs/cut/<name>.json in the vault.
type CutRunFiles interface {
	PutRunFile(ctx context.Context, kind, name, contents string) (string, error)
}

// cutRunName is the file a cut run is kept in, one for each run.
func cutRunName(c CutRun) string {
	return fmt.Sprintf("%s-%s.json", c.Began.UTC().Format("2006-01-02T150405Z"), c.Job)
}

// WriteCutRun writes c down in files.
func WriteCutRun(ctx context.Context, files CutRunFiles, c CutRun) error {
	if files == nil {
		return nil
	}
	c.RanFor = c.Ran.Round(time.Second).String()
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = files.PutRunFile(ctx, CutRunDir, cutRunName(c), string(body)+"\n")
	return err
}

// RunGuardedRestarter is a UnitRestarter that does not restart a unit of the
// factory while the follower has a model run under way: it waits for the marks
// to be gone, for at most Limit, and then restarts anyway. RestartFactoryUnits
// reads what it waited for and what it cut (RestartNotes).
type RunGuardedRestarter struct {
	Units UnitRestarter
	Runs  AgentRunMarks
	// Limit is how long it waits at most; zero is AgentRunWaitLimit.
	Limit time.Duration
	// Poll is how often it looks; zero is AgentRunPoll.
	Poll time.Duration
	// Wait waits d; nil is the real clock. Now is the clock; nil is time.Now.
	Wait func(ctx context.Context, d time.Duration) error
	Now  func() time.Time
	// Notice, when set, is told once when the restart begins to wait, so that a
	// landing that is quiet for minutes says why.
	Notice func(said string)

	mu    sync.Mutex
	notes map[string][]string
}

var _ UnitRestarter = (*RunGuardedRestarter)(nil)

func (g *RunGuardedRestarter) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

// TryRestart implements UnitRestarter.
func (g *RunGuardedRestarter) TryRestart(ctx context.Context, unit string) (bool, error) {
	if g.Runs == nil || unit != FollowerUnit {
		return g.Units.TryRestart(ctx, unit)
	}
	limit, poll, wait := g.Limit, g.Poll, g.Wait
	if limit <= 0 {
		limit = AgentRunWaitLimit
	}
	if poll <= 0 {
		poll = AgentRunPoll
	}
	if wait == nil {
		wait = waitFor
	}
	asked := g.now()
	var waited []AgentRun
	for {
		live, _, err := g.Runs.Open(ctx)
		if err != nil {
			g.note(unit, "the model runs under way could not be read, so %s was restarted without waiting: %v", unit, firstLine(err.Error()))
			break
		}
		if len(live) == 0 {
			if len(waited) > 0 {
				g.note(unit, "waited %s for %s to end before restarting %s", g.now().Sub(asked).Round(time.Second), jobNames(waited), unit)
			}
			break
		}
		if len(waited) == 0 && g.Notice != nil {
			g.Notice(fmt.Sprintf("restarting %s waits for %s to end (at most %s)", unit, jobNames(live), limit))
		}
		waited = live
		if g.now().Sub(asked) >= limit {
			g.note(unit, "%s was restarted after waiting %s: a run was cut: %s", unit, limit, runsLine(live, g.now()))
			break
		}
		if err := wait(ctx, poll); err != nil {
			return false, err
		}
	}
	return g.Units.TryRestart(ctx, unit)
}

// ReloadStale implements UnitRestarter.
func (g *RunGuardedRestarter) ReloadStale(ctx context.Context) ([]string, error) {
	return g.Units.ReloadStale(ctx)
}

// ReloadStaleSystem implements UnitRestarter.
func (g *RunGuardedRestarter) ReloadStaleSystem(ctx context.Context) ([]string, error) {
	return g.Units.ReloadStaleSystem(ctx)
}

// RestartNotes are the lines about unit's last restart beyond the plain one:
// what it waited for, or the run it cut. None for a restart that found no run.
func (g *RunGuardedRestarter) RestartNotes(unit string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	notes := g.notes[unit]
	delete(g.notes, unit)
	return notes
}

func (g *RunGuardedRestarter) note(unit, format string, args ...any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.notes == nil {
		g.notes = map[string][]string{}
	}
	g.notes[unit] = append(g.notes[unit], fmt.Sprintf(format, args...))
}

func jobNames(runs []AgentRun) string {
	names := make([]string, 0, len(runs))
	for _, r := range runs {
		names = append(names, r.Job)
	}
	return strings.Join(names, ", ")
}

func runsLine(runs []AgentRun, now time.Time) string {
	var lines []string
	for _, r := range runs {
		lines = append(lines, fmt.Sprintf("%s, begun %s, running %s", r.Job, r.Began.UTC().Format("15:04:05Z"), now.Sub(r.Began).Round(time.Second)))
	}
	return strings.Join(lines, "; ")
}

// FollowerUnit is the user unit of the follower, the one whose model runs the
// restart must not cut.
const FollowerUnit = "mw-view-follow.service"

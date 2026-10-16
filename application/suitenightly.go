package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// NightlySuiteJobName is the name of the follower's job that runs the nightly
// whole suite: its events' actor is suite@<host>.
const NightlySuiteJobName = "suite"

// DefaultNightlySuiteHour is the UTC hour from which the nightly suite is run
// when nothing says otherwise: about three or four in the morning in the US,
// when nobody is working a story.
const DefaultNightlySuiteHour = 8

// SuiteRecord is the newest result a rig's suite has kept: the UTC day of the
// file it is in (YYYY-MM-DD) and what it holds.
type SuiteRecord struct {
	Day    string
	Result SuiteResult
}

// SuiteHistory is the port the kept results of the whole suite are read back
// through: the vault's runs/suite/<rig>/<date>.json files. Reading changes
// nothing.
type SuiteHistory interface {
	// LatestSuite is the newest result kept for rig, and false when none is.
	LatestSuite(ctx context.Context, rig string) (SuiteRecord, bool, error)
}

// ParseSuiteRecord reads one kept result file, named for its day.
func ParseSuiteRecord(day, contents string) (SuiteRecord, error) {
	var result SuiteResult
	if err := json.Unmarshal([]byte(contents), &result); err != nil {
		return SuiteRecord{}, fmt.Errorf("the suite result of %s is not JSON: %w", day, err)
	}
	return SuiteRecord{Day: day, Result: result}, nil
}

// NightlySuite is the follower's job that runs the whole suite of every rig
// whose file says nightly_suite = true, once a UTC day, on the home only.
//
// From Hour on, a rig whose newest kept result is not of today's date is due.
// The passes run the due rigs one after the other, so two suites never run
// together; the pass is the job's, and the follower keeps a job in flight at
// most once. A rig is tried once a day by this follower, whether or not its run
// kept a result, so a suite that cannot be run does not become a run every pass.
type NightlySuite struct {
	// Hour is the UTC hour of the day from which the suite is due.
	Hour int
	// Rigs are the rigs checked out on this host, by name.
	Rigs map[string]string
	// Rules is where each rig's nightly_suite is read from.
	Rules EpicRules
	// History is where a day's kept result is looked for.
	History SuiteHistory
	// Home says whether this host is the home: only the home runs the suite.
	Home func(ctx context.Context) (bool, error)
	// Run does mw suite <rig> --file.
	Run func(ctx context.Context, rig string) error
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu    sync.Mutex
	tried map[string]string // rig -> the day it was last begun
}

func (n *NightlySuite) now() time.Time {
	if n.Now != nil {
		return n.Now().UTC()
	}
	return time.Now().UTC()
}

// Job is the follower's job: no event springs it; every pass asks whether a
// rig is due.
func (n *NightlySuite) Job() SpringJob {
	return SpringJob{
		Name: NightlySuiteJobName,
		Probe: func(ctx context.Context) (string, bool) {
			due, _ := n.due(ctx)
			if len(due) == 0 {
				return "", false
			}
			return "nightly suite of " + strings.Join(due, ", "), true
		},
		Run: n.run,
	}
}

// due is the rigs whose suite is to be run now, in name order. A rig whose
// rules cannot be read, or whose results cannot be, is left out and the error
// comes back beside the rest.
func (n *NightlySuite) due(ctx context.Context) ([]string, error) {
	now := n.now()
	if now.Hour() < n.Hour {
		return nil, nil
	}
	day := now.Format("2006-01-02")
	names := make([]string, 0, len(n.Rigs))
	for name := range n.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)

	n.mu.Lock()
	defer n.mu.Unlock()
	var due []string
	var errs []error
	for _, name := range names {
		if n.tried[name] == day {
			continue
		}
		rules, err := n.Rules.EpicRequirements(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading the rules of %s: %w", name, err))
			continue
		}
		if !rules.NightlySuite {
			continue
		}
		latest, kept, err := n.History.LatestSuite(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading the last suite of %s: %w", name, err))
			continue
		}
		if kept && latest.Day >= day {
			continue
		}
		due = append(due, name)
	}
	if len(due) == 0 {
		return nil, errors.Join(errs...)
	}
	// Home last, for it reads a file in the vault: asked only when a rig is due.
	if at, err := n.Home(ctx); err != nil || !at {
		return nil, errors.Join(append(errs, err)...)
	}
	return due, errors.Join(errs...)
}

// run runs each due rig's suite, one at a time, marking every one of them
// tried before the first begins so that a pass asked about while this one runs
// finds nothing more to do. A rig whose suite failed does not stop the next;
// the pass then fails, naming each.
func (n *NightlySuite) run(ctx context.Context) error {
	due, err := n.due(ctx)
	if len(due) == 0 {
		return err
	}
	day := n.now().Format("2006-01-02")
	n.mu.Lock()
	if n.tried == nil {
		n.tried = map[string]string{}
	}
	for _, name := range due {
		n.tried[name] = day
	}
	n.mu.Unlock()

	errs := []error{err}
	for _, name := range due {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		if err := n.Run(ctx, name); err != nil {
			errs = append(errs, fmt.Errorf("the suite of %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// SuiteProcesses is the port a nightly suite's process is begun and waited for
// through. The process lives apart from the follower: a follower that stops, as
// it does for every self-update, leaves it running (mw-gq6.395).
type SuiteProcesses interface {
	// Begin starts the suite of rig, unless one begun earlier, by this follower
	// or another, is still running: that one is the suite.
	Begin(ctx context.Context, rig string) error
	// Wait returns when the suite of rig ends: nil if it passed, an error if it
	// failed or ended without saying. A ctx that ends stops the wait, not the
	// suite.
	Wait(ctx context.Context, rig string) error
}

// DetachedSuite runs the nightly suite of a rig in a process apart from the
// follower, and adopts one a follower before this had begun.
type DetachedSuite struct {
	Processes SuiteProcesses
}

// Run begins the suite of rig, or finds the one running, and waits for it.
func (d DetachedSuite) Run(ctx context.Context, rig string) error {
	if err := d.Processes.Begin(ctx, rig); err != nil {
		return LeftRunning(ctx, fmt.Errorf("beginning the suite: %w", err))
	}
	return LeftRunning(ctx, d.Processes.Wait(ctx, rig))
}

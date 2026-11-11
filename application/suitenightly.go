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

	"github.com/Jonathan-A-White/millwright/domain"
)

// NightlySuiteJobName is the name of the follower's job that runs the nightly
// whole suite: its events' actor is suite@<host>.
const NightlySuiteJobName = "suite"

// DefaultNightlySuiteHour is the UTC hour from which the nightly suite is run
// when nothing says otherwise: about three or four in the morning in the US.
// Stories are worked then all the same: the suite ran 16 to 131 minutes (median
// 36) from 08:00 to 09:44 UTC over the 21 nights 2026-10-13 to 2026-11-02, and 7
// of the Laptop's 111 gates of the 7 days to 2026-11-03 fell in 08:00 to 12:00
// UTC. So the suite shares the host with gates and Builders; see
// docs/dispatch-gates.md.
const DefaultNightlySuiteHour = 8

// SuiteEveryMin is the shortest suite_every a rig's file may name.
const SuiteEveryMin = time.Hour

// SuiteFallbackAfter is how old the newest kept result of a rig with a suite
// host must be before the home runs its nightly all the same: a suite host that
// is down costs one day, not the suite (mw-t5shinh.15).
const SuiteFallbackAfter = 26 * time.Hour

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

// Began is when the run of the result began, from the time the result keeps. A
// result that keeps none, or none that reads, began at the start of its day.
func (r SuiteRecord) Began() time.Time {
	if t, err := time.Parse(time.RFC3339, r.Result.Started); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02", r.Day); err == nil {
		return t
	}
	return time.Time{}
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
// A rig whose file also names suite_every and suite_host is run by the follower
// on that host instead, whenever the newest kept result began more than
// suite_every ago and main's head is not that result's sha, at any hour; the
// home then runs its nightly only when the newest kept result, whoever kept it,
// began more than 26 hours ago (mw-t5shinh.15).
//
// From Hour on, a rig whose newest kept result is not of today's date is due.
// The passes run the due rigs one after the other, so two suites never run
// together; the pass is the job's, and the follower keeps a job in flight at
// most once. A rig is tried once a day by this follower, whether or not its run
// kept a result, so a suite that cannot be run does not become a run every pass.
type NightlySuite struct {
	// Hour is the UTC hour of the day from which the suite is due.
	Hour int
	// Rigs are the rigs checked out on this host, by name, as the follower
	// started; ReadRigs, when set, is asked on every pass for the rigs as they
	// are now, so that a rig added while the follower runs has its night's
	// suite too (mw-gq6.748). A read that fails leaves Rigs.
	Rigs     map[string]string
	ReadRigs func() (map[string]string, error)
	// Rules is where each rig's nightly_suite is read from.
	Rules EpicRules
	// History is where a day's kept result is looked for.
	History SuiteHistory
	// Home says whether this host is the home: only the home runs the nightly.
	Home func(ctx context.Context) (bool, error)
	// Host is this host's name, which a rig's suite_host is compared with.
	Host string
	// Heads reads the head of a rig's main, as the suite host asks whether it has
	// moved since the newest result. Nil is a head that is not known to be the
	// result's, so that the time alone decides.
	Heads RigHeads
	// Run does mw suite <rig> --file.
	Run func(ctx context.Context, rig string) error
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu    sync.Mutex
	tried map[string]string // rig -> the day it was last begun
	// triedAt is when a suite host's run of a rig was last begun, for the rigs
	// that run every suite_every: a run that keeps no result is not begun again
	// before suite_every has passed.
	triedAt map[string]time.Time
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
		// A failed night's suite is told to the Mayor (mw-gq6.531).
		TellFailure: true,
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
	day := now.Format("2006-01-02")
	rigs := n.Rigs
	if n.ReadRigs != nil {
		if now, err := n.ReadRigs(); err == nil {
			rigs = now
		}
	}
	names := make([]string, 0, len(rigs))
	for name := range rigs {
		names = append(names, name)
	}
	sort.Strings(names)

	n.mu.Lock()
	defer n.mu.Unlock()
	var due, atHome []string
	var errs []error
	for _, name := range names {
		rules, err := n.Rules.EpicRequirements(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading the rules of %s: %w", name, err))
			continue
		}
		if !rules.NightlySuite {
			continue
		}
		suiteHost := rules.SuiteEvery > 0 && rules.SuiteHost != ""
		if suiteHost && rules.SuiteHost == n.Host {
			// The suite host runs it whenever it is due, at any hour, wherever the home is.
			if at, ok := n.triedAt[name]; ok && now.Sub(at) < rules.SuiteEvery {
				continue
			}
			moved, err := n.movedSince(ctx, name, rigs[name], rules, now)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if moved {
				due = append(due, name)
			}
			continue
		}
		if now.Hour() < n.Hour || n.tried[name] == day {
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
		if suiteHost && kept && now.Sub(latest.Began()) <= SuiteFallbackAfter {
			// Its suite host keeps the suite; the home steps in only when that host has gone quiet.
			continue
		}
		atHome = append(atHome, name)
	}
	if len(atHome) > 0 {
		// Home last, for it reads a file in the vault: asked only when a rig is due.
		if at, err := n.Home(ctx); err != nil {
			errs = append(errs, err)
		} else if at {
			due = append(due, atHome...)
			sort.Strings(due)
		}
	}
	if len(due) == 0 {
		return nil, errors.Join(errs...)
	}
	return due, errors.Join(errs...)
}

// movedSince says whether the suite host is to run the suite of rig now: no
// result is kept, or the newest began more than suite_every ago and main's head
// is not its sha.
func (n *NightlySuite) movedSince(ctx context.Context, rig, dir string, rules domain.EpicRequirements, now time.Time) (bool, error) {
	latest, kept, err := n.History.LatestSuite(ctx, rig)
	if err != nil {
		return false, fmt.Errorf("reading the last suite of %s: %w", rig, err)
	}
	if !kept {
		return true, nil
	}
	if now.Sub(latest.Began()) <= rules.SuiteEvery {
		return false, nil
	}
	if n.Heads == nil {
		return true, nil
	}
	head, err := n.Heads.Resolve(ctx, dir, "")
	if err != nil {
		return false, fmt.Errorf("reading the head of %s's main: %w", rig, err)
	}
	return head.Commit != latest.Result.Sha, nil
}

// run runs each due rig's suite, one at a time, marking every one of them
// tried before the first begins so that a pass asked about while this one runs
// finds nothing more to do. A rig whose suite failed does not stop the next;
// the pass then fails, naming each.
func (n *NightlySuite) run(ctx context.Context) error {
	due, err := n.due(ctx)
	if len(due) == 0 {
		if err != nil {
			return err
		}
		return SkippedPass("no suite run: no rig is due (none asks for it, each has its result, or this host is neither the home nor a rig's suite host)")
	}
	day := n.now().Format("2006-01-02")
	n.mu.Lock()
	if n.tried == nil {
		n.tried = map[string]string{}
	}
	if n.triedAt == nil {
		n.triedAt = map[string]time.Time{}
	}
	at := n.now()
	for _, name := range due {
		n.tried[name] = day
		n.triedAt[name] = at
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

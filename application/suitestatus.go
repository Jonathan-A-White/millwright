package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SuiteHeading heads the section naming, for each rig that runs the nightly
// whole suite, how its last run went. The report leaves it out when no rig
// does.
const SuiteHeading = "SUITE"

// SuiteStale is how many days old the newest kept result of a rig's suite may
// be before mw status says it has not run: the nightly run is one a day, so a
// result of yesterday is only the night before.
const SuiteStale = 2

// SuiteBug is where one failing file of a suite result stands: on an open bead
// under the suite epic (ID set, Closed zero), fixed by a bead closed since the
// run began (ID set, Closed the time), or on no bead at all (ID empty).
type SuiteBug struct {
	ID     string
	Closed time.Time
}

// SuiteStatus is one rig's line under SUITE: the newest result kept for it, the
// beads that hold its failing files, and how many days ago that was.
type SuiteStatus struct {
	Rig string
	// Kept says a result was found; a rig that never ran has none.
	Kept bool
	Day  string
	Sha  string
	// Failing is how many files failed, and Bugs says, for each file the run
	// named and in the order they are, where its bug stands. It is empty when
	// the suite epic could not be read, so that nothing is claimed.
	Failing int
	Bugs    []SuiteBug
	// DaysAgo is how many UTC days before the report the result is of.
	DaysAgo int
}

// Line is the line printed for the rig.
func (u SuiteStatus) Line() string {
	if !u.Kept {
		return fmt.Sprintf("%s: never run", u.Rig)
	}
	line := fmt.Sprintf("%s %s %s: all pass", u.Rig, u.Day, short(u.Sha))
	if u.Failing > 0 {
		line = fmt.Sprintf("%s %s %s: %d failing", u.Rig, u.Day, short(u.Sha), u.Failing)
		if len(u.Bugs) > 0 {
			line += ": " + u.bugsLine()
		}
	}
	if u.DaysAgo >= SuiteStale {
		line += fmt.Sprintf(" · not run for %d days", u.DaysAgo)
	}
	return line
}

// bugsLine says where each failing file's bug stands: an open one by its id, a
// mended one as fixed (with when it landed), a file with none as unfiled. A
// fixed one keeps the line until the next run, which is the proof.
func (u SuiteStatus) bugsLine() string {
	var said []string
	fixed := false
	for _, bug := range u.Bugs {
		switch {
		case bug.ID == "":
			said = append(said, "unfiled")
		case bug.Closed.IsZero():
			said = append(said, bug.ID)
		default:
			fixed = true
			said = append(said, fmt.Sprintf("fixed by %s (landed %s)", bug.ID, u.landed(bug.Closed)))
		}
	}
	line := strings.Join(said, ", ")
	if fixed {
		line += ", until the next run"
	}
	return line
}

// landed is when a bug was closed: the time of day when that is the day of the
// run, else with its date.
func (u SuiteStatus) landed(closed time.Time) string {
	closed = closed.UTC()
	if closed.Format("2006-01-02") == u.Day {
		return closed.Format("15:04Z")
	}
	return closed.Format("2006-01-02 15:04Z")
}

// suiteStatuses reads, for each of this host's rigs whose file says
// nightly_suite = true, the newest kept result of its suite. A rig whose result
// cannot be read is not a reason to refuse the rest of the report: it is left
// out. Nothing is written.
func (s Status) suiteStatuses(ctx context.Context) []SuiteStatus {
	if s.Suites == nil || s.Rules == nil {
		return nil
	}
	names := make([]string, 0, len(s.Rigs))
	for name := range s.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	today := s.now().UTC().Truncate(24 * time.Hour)
	var out []SuiteStatus
	for _, name := range names {
		rules, err := s.Rules.EpicRequirements(ctx, name)
		if err != nil || !rules.NightlySuite {
			continue
		}
		latest, kept, err := s.Suites.LatestSuite(ctx, name)
		if err != nil {
			continue
		}
		one := SuiteStatus{Rig: name, Kept: kept}
		if kept {
			one.Day, one.Sha = latest.Day, latest.Result.Sha
			one.Failing = len(latest.Result.Failing) + len(latest.Result.Unnamed)
			if !latest.Result.Passed && one.Failing == 0 {
				one.Failing = 1
			}
			if day, err := time.Parse("2006-01-02", latest.Day); err == nil {
				one.DaysAgo = int(today.Sub(day) / (24 * time.Hour))
			}
			one.Bugs = s.suiteBugs(ctx, rules.SuiteEpic, latest.Day, latest.Result)
		}
		out = append(out, one)
	}
	return out
}

// suiteBugs says, for each file the result says failed and in the order the
// files are, where its bug stands under the suite epic: open, closed since the
// run began, or not filed. A suite epic that is not named or cannot be read
// says nothing of any.
func (s Status) suiteBugs(ctx context.Context, epic, day string, result SuiteResult) []SuiteBug {
	if strings.TrimSpace(epic) == "" || len(result.Failing) == 0 {
		return nil
	}
	detail, err := s.Tracker.ShowEpic(ctx, strings.TrimSpace(epic))
	if err != nil {
		return nil
	}
	since := suiteStarted(day, result)
	bugs := make([]SuiteBug, 0, len(result.Failing))
	for _, file := range result.Failing {
		if story, open := openSuiteBug(detail, file.File); open {
			bugs = append(bugs, SuiteBug{ID: story.Story.ID})
		} else if story, fixed := fixedSuiteBug(detail, file.File, since); fixed {
			bugs = append(bugs, SuiteBug{ID: story.Story.ID, Closed: story.ClosedAt})
		} else {
			bugs = append(bugs, SuiteBug{})
		}
	}
	return bugs
}

// suiteStarted is when the run began: the result's own start, else the start of
// the day it is kept under.
func suiteStarted(day string, result SuiteResult) time.Time {
	if at, err := time.Parse(time.RFC3339, result.Started); err == nil {
		return at
	}
	at, _ := time.Parse("2006-01-02", day)
	return at
}

// writeSuite prints the SUITE section: a line for each rig, left out when the
// report holds none.
func (r StatusReport) writeSuite(b *strings.Builder) {
	if len(r.Suites) == 0 {
		return
	}
	clip(b, SuiteHeading)
	for _, one := range r.Suites {
		wrapInto(b, "  ", one.Line())
	}
	b.WriteString("\n")
}

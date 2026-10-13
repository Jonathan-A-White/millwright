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

// SuiteStatus is one rig's line under SUITE: the newest result kept for it, the
// open beads that hold its failing files, and how many days ago that was.
type SuiteStatus struct {
	Rig string
	// Kept says a result was found; a rig that never ran has none.
	Kept bool
	Day  string
	Sha  string
	// Failing is how many files failed, and Beads the open bugs under the
	// suite epic that name them.
	Failing int
	Beads   []string
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
		if len(u.Beads) > 0 {
			line += ": " + strings.Join(u.Beads, ", ")
		}
	}
	if u.DaysAgo >= SuiteStale {
		line += fmt.Sprintf(" · not run for %d days", u.DaysAgo)
	}
	return line
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
			one.Beads = s.suiteBeads(ctx, rules.SuiteEpic, latest.Result)
		}
		out = append(out, one)
	}
	return out
}

// suiteBeads names the open bugs under the suite epic that hold the files the
// result says failed, in the order the files are. A bead that cannot be found
// is simply not named.
func (s Status) suiteBeads(ctx context.Context, epic string, result SuiteResult) []string {
	if strings.TrimSpace(epic) == "" || len(result.Failing) == 0 {
		return nil
	}
	detail, err := s.Tracker.ShowEpic(ctx, strings.TrimSpace(epic))
	if err != nil {
		return nil
	}
	var ids []string
	for _, file := range result.Failing {
		if story, open := openSuiteBug(detail, file.File); open {
			ids = append(ids, story.Story.ID)
		}
	}
	return ids
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

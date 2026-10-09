package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// TesterStories is the tracker narrowed to what mw tester report reads: every
// story touched since a moment, closed ones included, and their comments.
type TesterStories interface {
	// StoriesSince lists every story, closed ones included and epics left out,
	// filed, changed or closed at since or later, each with its own epic's
	// defaults overlaid. A story the tracker gives no times for is listed.
	StoriesSince(ctx context.Context, since time.Time) ([]StoryDetail, error)
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
}

// TesterRunFiles is the vault narrowed to what mw tester report reads: the
// seat's ledger, which says what landed, and a Tester run's result, which says
// what it burned.
type TesterRunFiles interface {
	ReadLedger(ctx context.Context, seat string) ([]string, error)
	ReadRunFile(ctx context.Context, storyID, name string) (string, error)
}

// TesterReport sums a Tester trial per rig: the landings in it, the Tester
// runs, what they found, the [bug] stories filed from a finding, and each run's
// fuel from its result. It reads and writes nothing else.
type TesterReport struct {
	Tracker TesterStories
	Files   TesterRunFiles
	// Trial is the home's [tester] table; its rigs are the ones reported, and
	// with none, every rig a Tester ran on.
	Trial TesterTrial
	// Seat is whose ledger says what landed.
	Seat string
	// Since is where the report starts; zero is TesterTrialDays before the
	// trial's until, or before now when there is no until.
	Since time.Time
	Now   func() time.Time
	Out   io.Writer
}

// TesterRun is one Tester story closed in the trial: what it tested, what it
// found, and what it burned, as its result says.
type TesterRun struct {
	ID       string
	Landed   string
	Findings TesterFindings
	// Found says the landed story carries FINDINGS; FuelKnown that the run's
	// result could be read.
	Found     bool
	Fuel      Fuel
	FuelKnown bool
}

// TesterRigReport is one rig's share of the trial.
type TesterRigReport struct {
	Rig      string
	Landings []string
	Runs     []TesterRun
	// Bugs are the [bug] stories whose description names a Tester finding.
	Bugs []string
}

// Findings is every finding of the rig's Tester runs.
func (r TesterRigReport) Findings() TesterFindings {
	var all TesterFindings
	for _, run := range r.Runs {
		all.Bug += run.Findings.Bug
		all.Taste += run.Findings.Taste
	}
	return all
}

// TesterReportResult is what mw tester report found.
type TesterReportResult struct {
	Since, Until time.Time
	Rigs         []TesterRigReport
}

// Run reads the trial and prints the report.
func (r TesterReport) Run(ctx context.Context) (TesterReportResult, error) {
	if r.Tracker == nil || r.Files == nil {
		return TesterReportResult{}, fmt.Errorf("reporting the Tester trial: it needs the tracker and the vault")
	}
	result := TesterReportResult{Since: r.since(), Until: r.Trial.Until}
	stories, err := r.Tracker.StoriesSince(ctx, result.Since)
	if err != nil {
		return result, fmt.Errorf("reporting the Tester trial: %w", err)
	}
	ledger, err := r.Files.ReadLedger(ctx, r.Seat)
	if err != nil {
		return result, fmt.Errorf("reporting the Tester trial: the %s seat's ledger: %w", r.Seat, err)
	}

	rigs := map[string]*TesterRigReport{}
	rigOf := func(name string) *TesterRigReport {
		if rigs[name] == nil {
			rigs[name] = &TesterRigReport{Rig: name}
		}
		return rigs[name]
	}
	listed := len(r.Trial.Rigs) > 0
	for _, name := range r.Trial.Rigs {
		rigOf(strings.TrimSpace(name))
	}
	inTrial := func(rig string) bool { return rigs[rig] != nil || !listed }

	var landedIDs []string
	for _, d := range stories {
		rig := d.Merged().Rig
		if d.IsEpic || rig == "" || !inTrial(rig) {
			continue
		}
		closedInTrial := d.Closed() && (d.ClosedAt.IsZero() || !d.ClosedAt.Before(result.Since))
		switch {
		case IsTesterStory(d):
			if !closedInTrial {
				continue
			}
			run := TesterRun{ID: d.Story.ID, Landed: TesterLanded(d)}
			run.Fuel, run.FuelKnown = r.fuel(ctx, d)
			report := rigOf(rig)
			report.Runs = append(report.Runs, run)
			if run.Landed != "" {
				landedIDs = append(landedIDs, run.Landed)
			}
		case closedInTrial && !isDemo(d) && ledgerLands(ledger, d.Story.ID):
			rigOf(rig).Landings = append(rigOf(rig).Landings, d.Story.ID)
		}
		if domain.IsBugStory(d.Type, d.Story.Title) && strings.Contains(strings.ToLower(d.Description), strings.ToLower(TesterFindingMarker)) {
			rigOf(rig).Bugs = append(rigOf(rig).Bugs, d.Story.ID)
		}
	}

	if len(landedIDs) > 0 {
		comments, err := r.Tracker.StoriesComments(ctx, landedIDs)
		if err != nil {
			return result, fmt.Errorf("reporting the Tester trial: the comments on the landed stories: %w", err)
		}
		for _, report := range rigs {
			for i := range report.Runs {
				run := &report.Runs[i]
				run.Findings, run.Found = newestFindings(comments[run.Landed], time.Time{})
			}
		}
	}

	names := make([]string, 0, len(rigs))
	for name := range rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result.Rigs = append(result.Rigs, *rigs[name])
	}
	if r.Out != nil {
		fmt.Fprint(r.Out, result.String())
	}
	return result, nil
}

// ledgerLands reports whether any line of ledger lands id.
func ledgerLands(ledger []string, id string) bool {
	for _, line := range ledger {
		if LedgerLandsStory(line, id) {
			return true
		}
	}
	return false
}

// fuel reads what a Tester run burned off its result.
func (r TesterReport) fuel(ctx context.Context, d StoryDetail) (Fuel, bool) {
	attempt := d.Attempts
	if attempt < 1 {
		attempt = 1
	}
	printed, err := r.Files.ReadRunFile(ctx, d.Story.ID, ResultFileNameForAttempt(attempt))
	if err != nil || strings.TrimSpace(printed) == "" {
		return Fuel{}, false
	}
	result, err := ReadSessionResult(printed)
	if err != nil {
		return Fuel{}, false
	}
	return result.Fuel, true
}

// since is where the report starts.
func (r TesterReport) since() time.Time {
	switch {
	case !r.Since.IsZero():
		return r.Since
	case !r.Trial.Until.IsZero():
		return r.Trial.Until.AddDate(0, 0, -TesterTrialDays)
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	return now.AddDate(0, 0, -TesterTrialDays)
}

// String is the report as mw tester report prints it, for the demo to paste.
func (r TesterReportResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tester trial since %s", r.Since.UTC().Format("2006-01-02 15:04 UTC"))
	if !r.Until.IsZero() {
		fmt.Fprintf(&b, ", until %s", r.Until.UTC().Format("2006-01-02 15:04 UTC"))
	}
	b.WriteString("\n")
	if len(r.Rigs) == 0 {
		b.WriteString("\nno rig is in the trial, and no Tester ran\n")
	}
	for _, rig := range r.Rigs {
		fmt.Fprintf(&b, "\n%s\n", rig.Rig)
		fmt.Fprintf(&b, "  %-22s %d\n", "landings in the trial", len(rig.Landings))
		fmt.Fprintf(&b, "  %-22s %d\n", "Tester runs", len(rig.Runs))
		found := rig.Findings()
		fmt.Fprintf(&b, "  %-22s %d (%d bug, %d taste)\n", "findings", found.Count(), found.Bug, found.Taste)
		bugs := fmt.Sprintf("%d", len(rig.Bugs))
		if len(rig.Bugs) > 0 {
			bugs += ": " + strings.Join(rig.Bugs, ", ")
		}
		fmt.Fprintf(&b, "  %-22s %s\n", "[bug] from a finding", bugs)
		if len(rig.Runs) == 0 {
			continue
		}
		b.WriteString("  fuel per Tester run\n")
		total := 0
		for _, run := range rig.Runs {
			found := "no FINDINGS"
			if run.Found {
				found = run.Findings.Counted()
			}
			fuel := "fuel unknown: no result"
			if run.FuelKnown {
				fuel = Thousands(run.Fuel.Total()) + " tokens"
				total += run.Fuel.Total()
			}
			fmt.Fprintf(&b, "    %s  tested %s  %s  %s\n", run.ID, orNone(run.Landed), found, fuel)
		}
		fmt.Fprintf(&b, "  %-22s %s tokens\n", "Tester fuel in all", Thousands(total))
	}
	return b.String()
}

// orNone is text, or "(none named)" when it is empty.
func orNone(text string) string {
	if text == "" {
		return "(none named)"
	}
	return text
}

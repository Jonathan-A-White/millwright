package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The metadata a story carries once the Mayor has named why it failed
// (mw cause): the class, the reason in the Mayor's words, and the bead that
// mends it. They are metadata so that causes can be counted.
const (
	CauseField    = "cause"
	CauseWhyField = "cause_why"
	CauseFixField = "cause_fix"
)

// AttemptCausesField is the metadata that keeps the class mw cause named for
// each of the story's attempts, attempt:class pairs comma separated
// ("1:process,2:test,3:code"): CauseField holds only the latest, and an attempt
// that failed for what is not the story's own code is not counted toward
// max_attempts (mw-gq6.450).
const AttemptCausesField = "attempt_causes"

// OwnCause is the class of cause that is the story's own code: an attempt named
// it, or named nothing, counts toward max_attempts.
const OwnCause = "code"

// ParseAttemptCauses is the AttemptCausesField metadata read back: the class
// named for each attempt, by attempt. A pair it cannot read is left out; empty
// is nil.
func ParseAttemptCauses(text string) map[int]string {
	var causes map[int]string
	for _, pair := range strings.Split(text, ",") {
		at, class, ok := strings.Cut(strings.TrimSpace(pair), ":")
		attempt, err := strconv.Atoi(strings.TrimSpace(at))
		if !ok || err != nil || attempt < 1 || strings.TrimSpace(class) == "" {
			continue
		}
		if causes == nil {
			causes = map[int]string{}
		}
		causes[attempt] = strings.TrimSpace(class)
	}
	return causes
}

// attemptCausesText is causes as the AttemptCausesField metadata keeps them,
// oldest attempt first.
func attemptCausesText(causes map[int]string) string {
	attempts := make([]int, 0, len(causes))
	for attempt := range causes {
		attempts = append(attempts, attempt)
	}
	sort.Ints(attempts)
	pairs := make([]string, len(attempts))
	for i, attempt := range attempts {
		pairs[i] = fmt.Sprintf("%d:%s", attempt, causes[attempt])
	}
	return strings.Join(pairs, ",")
}

// UncountedAttempt is one of a story's attempts that does not count toward
// max_attempts, and why: the class mw cause named for it.
type UncountedAttempt struct {
	Attempt int
	Cause   string
}

// OwnAttempts is how many of the story's attempts count toward max_attempts:
// those mw cause named code, or named nothing (mw-gq6.450). The attempts that
// do not count, the ones named test, tooling or process, come back with their
// class, oldest first.
func OwnAttempts(d StoryDetail) (int, []UncountedAttempt) {
	own := 0
	var uncounted []UncountedAttempt
	for attempt := 1; attempt <= d.Attempts; attempt++ {
		if class := d.AttemptCauses[attempt]; class == "" || class == OwnCause {
			own++
		} else {
			uncounted = append(uncounted, UncountedAttempt{Attempt: attempt, Cause: class})
		}
	}
	return own, uncounted
}

// CauseClasses are the four classes a failed story's cause falls in.
var CauseClasses = []string{"code", "test", "tooling", "process"}

// CausePrefix begins the comment mw cause leaves on the story, with the class
// in the brackets that follow it.
const CausePrefix = "CAUSE ("

// CausesMissingHeading heads the section of mw status naming the stories refused
// lately that no cause has been recorded for. The report leaves it out when
// there are none.
const CausesMissingHeading = "CAUSES MISSING"

// CausesMissingWindow is how far back mw status looks for a refusal.
const CausesMissingWindow = 7 * 24 * time.Hour

// CauseLine is the command that names a story's cause, with the class and the
// reason left for the person to fill in: what a Refused or Blocked mail ends
// with.
func CauseLine(id string) string {
	return fmt.Sprintf(`mw cause %s <class> "<why>"`, id)
}

// NoCauseReminder is what mw retry says of a story that has no cause recorded.
func NoCauseReminder(id string) string {
	return "no cause recorded yet: " + CauseLine(id)
}

// validCauseClass is class, lower-cased and trimmed, and whether it is one of
// the four.
func validCauseClass(class string) (string, bool) {
	class = strings.ToLower(strings.TrimSpace(class))
	for _, known := range CauseClasses {
		if class == known {
			return class, true
		}
	}
	return class, false
}

// Cause records why a story failed: its class (one of CauseClasses), the reason
// in words, and, when there is one, the bead that fixes it. The class, reason
// and fix are written as the story's metadata in one write, the class kept
// besides as the cause of the story's latest attempt (AttemptCausesField), and
// one comment beginning "CAUSE (<class>):" says the same to a person reading
// the story. A
// story named twice has its class, reason and fix replaced and a second
// comment added. Nothing else of the story is touched: not its status, its
// claim nor its attempts.
//
// Every refusal comes before the write: an unknown class, an empty reason, a
// story or a fix the tracker does not know changes nothing.
type Cause struct {
	Tracker WorkTracker

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// CauseReport is what one run of mw cause recorded.
type CauseReport struct {
	StoryID, Class, Why, Fix string
}

// Run names the cause of story id. fix is the bead that mends it, or empty.
func (c Cause) Run(ctx context.Context, id, class, why, fix string) (CauseReport, error) {
	report := CauseReport{StoryID: strings.TrimSpace(id)}
	if c.Tracker == nil {
		return report, fmt.Errorf("naming a cause: there is no work tracker to name it in")
	}
	if report.StoryID == "" {
		return report, fmt.Errorf("naming a cause: which story?")
	}
	class, ok := validCauseClass(class)
	if !ok {
		return report, fmt.Errorf("naming the cause of %s: %q is not a class of cause: use one of %s",
			report.StoryID, class, strings.Join(CauseClasses, ", "))
	}
	why = strings.Join(strings.Fields(why), " ")
	if why == "" {
		return report, fmt.Errorf("naming the cause of %s: say why, in a few words", report.StoryID)
	}
	report.Class, report.Why, report.Fix = class, why, strings.TrimSpace(fix)

	story, err := c.Tracker.ShowStory(ctx, report.StoryID)
	if err != nil {
		return report, fmt.Errorf("naming the cause of %s: %w", report.StoryID, err)
	}
	if report.Fix != "" {
		if _, err := c.Tracker.ShowStory(ctx, report.Fix); err != nil {
			return report, fmt.Errorf("naming the cause of %s: the fix %s: %w", report.StoryID, report.Fix, err)
		}
	}

	causes := map[int]string{}
	for attempt, named := range story.AttemptCauses {
		causes[attempt] = named
	}
	causes[max(story.Attempts, 1)] = class
	fields := map[string]string{CauseField: class, CauseWhyField: why, AttemptCausesField: attemptCausesText(causes)}
	if report.Fix != "" || story.CauseFix != "" {
		fields[CauseFixField] = report.Fix
	}
	if err := c.Tracker.SetStoryMetadata(ctx, report.StoryID, fields); err != nil {
		return report, fmt.Errorf("naming the cause of %s: %w", report.StoryID, err)
	}

	comment := fmt.Sprintf("%s%s): %s", CausePrefix, class, why)
	if report.Fix != "" {
		comment += "\nFix: " + report.Fix
	}
	if err := c.Tracker.CommentOnStory(ctx, report.StoryID, comment); err != nil {
		return report, fmt.Errorf("naming the cause of %s: the cause is recorded, but the comment saying it could not be written: %w", report.StoryID, err)
	}
	if c.Out != nil {
		fmt.Fprint(c.Out, report.String())
	}
	return report, nil
}

// String is the report as a person reads it.
func (r CauseReport) String() string {
	s := fmt.Sprintf("cause of %s: %s: %s\n", r.StoryID, r.Class, r.Why)
	if r.Fix != "" {
		s += fmt.Sprintf("  fixed by %s\n", r.Fix)
	}
	return s
}

// CauseListRow is one story a cause is recorded for, as mw cause list prints it.
type CauseListRow struct {
	StoryID, Rig, Class, Why, Fix string
}

// String is the row as one line: id, rig, class, why and the fix bead, "-" when
// there is none.
func (r CauseListRow) String() string {
	return strings.Join([]string{r.StoryID, r.Rig, r.Class, r.Why, orElse(r.Fix, "-")}, " · ")
}

// CauseList reads which story got which class of cause: every story touched
// since Since that mw cause has named, one line each, oldest id first, for one
// rig when Rig is set. It reads the tracker and nothing else: no session, no
// tokens. The counts per rig are mw report's; this is the stories behind them.
type CauseList struct {
	Tracker TesterStories
	// Since is where the window starts; zero lists every story.
	Since time.Time
	// Rig, when set, narrows the list to that rig.
	Rig string
	// Out is where the lines are printed. A nil Out prints nothing.
	Out io.Writer
}

// Run lists the caused stories and prints them.
func (l CauseList) Run(ctx context.Context) ([]CauseListRow, error) {
	if l.Tracker == nil {
		return nil, fmt.Errorf("listing causes: there is no work tracker to read them from")
	}
	stories, err := l.Tracker.StoriesSince(ctx, l.Since)
	if err != nil {
		return nil, fmt.Errorf("listing causes: %w", err)
	}
	var rows []CauseListRow
	for _, d := range stories {
		if d.IsEpic || strings.TrimSpace(d.Cause) == "" {
			continue
		}
		rig := orElse(d.Merged().Rig, ReportUnknown)
		if l.Rig != "" && rig != l.Rig {
			continue
		}
		rows = append(rows, CauseListRow{StoryID: d.Story.ID, Rig: rig, Class: d.Cause,
			Why: strings.Join(strings.Fields(d.CauseWhy), " "), Fix: d.CauseFix})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StoryID < rows[j].StoryID })
	if l.Out != nil {
		if len(rows) == 0 {
			fmt.Fprintln(l.Out, "no cause named in the window")
		}
		for _, row := range rows {
			fmt.Fprintln(l.Out, row)
		}
	}
	return rows, nil
}

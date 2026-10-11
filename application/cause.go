package application

import (
	"context"
	"fmt"
	"io"
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
// and fix are written as the story's metadata in one write, and one comment
// beginning "CAUSE (<class>):" says the same to a person reading the story. A
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

	fields := map[string]string{CauseField: class, CauseWhyField: why}
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

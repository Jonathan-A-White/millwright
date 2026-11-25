package application

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"
)

// changelogWhy runs the rig's changelog_check on story s's What's new line as
// the batch will write it, before s is merged, and says why s must be set aside
// when the check refuses it; it is empty when the line passes, or when the check
// was not run. A story's own close-out ran the check on the line it then had
// (mw-gq6.1610), but the line is the newest What's new comment read at landing,
// and a comment added after the story was queued is first met here: written into
// the batch it would turn the batch's tests red, held for the green stories with
// it (mw-gq6.1745). A check that could not be run is a note and the story goes on: the batch's
// own tests stand after it.
func (b BatchLand) changelogWhy(ctx context.Context, rigDir string, s BatchStory, rules domain.EpicRequirements, report *BatchReport, nr *NextReport) string {
	if rules.ChangelogCheck == "" || len(rules.VersionFiles) == 0 || len(rules.ChangelogFiles) == 0 || s.OnTarget {
		return ""
	}
	if why, done := report.changelogChecked[s.ID]; done {
		return why
	}
	if report.changelogChecked == nil {
		report.changelogChecked = map[string]string{}
	}
	report.changelogChecked[s.ID] = ""
	c := b.storyCloseOut(s, rigDir)
	// The branch as the origin has it: a story queued on another host has none
	// here of its own.
	c.branch = s.Branch
	refusal, refused := b.Next.changelogCheckRefusal(ctx, c, nr, report.Base)
	switch {
	case !refused:
		return ""
	case refusal.Reason != ReasonTestsFail:
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %s", s.ID, refusal.Why))
		return ""
	}
	report.changelogChecked[s.ID] = refusal.Why
	return refusal.Why
}

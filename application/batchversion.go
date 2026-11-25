package application

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"
)

// raiseVersion raises the version once for the batch as merged in the landing
// worktree dir, with one changelog entry for each story, as the gate on it is to
// find it. It reports whether the batch goes on; when it does not, Held says why.
func (b BatchLand) raiseVersion(ctx context.Context, dir, rigDir string, rules domain.EpicRequirements, report *BatchReport, nr *NextReport) bool {
	n := b.Next
	// A batch every branch of which was on the target already brings nothing
	// new: no version is raised for it and no What's new line written.
	if len(rules.VersionFiles) > 0 && !report.allOnTarget() {
		bump := VersionBump{Files: rules.VersionFiles, Before: report.Base, Branch: "HEAD", StoryID: report.Label()}
		for _, s := range report.Merged {
			// One story of the batch asking for the minor raises it for all.
			if s.path.Bump == domain.BumpMinor && !s.OnTarget {
				bump.Minor = true
			}
		}
		if len(rules.ChangelogFiles) > 0 {
			bump.Changelog = rules.ChangelogFiles
			for _, s := range report.Merged {
				if s.OnTarget {
					report.Notes = append(report.Notes, fmt.Sprintf("%s was on %s already, so its What's new line is not written again", s.ID, report.Target))
					continue
				}
				entry, err := n.changelogEntry(ctx, b.storyCloseOut(s, rigDir), nr)
				if err != nil {
					report.Held = fmt.Sprintf("the changelog entry of %s could not be read: %v", s.ID, err)
					return false
				}
				if entry.Text != "" {
					bump.Entries = append(bump.Entries, entry)
				}
			}
		}
		bumped, err := n.Landing.BumpVersion(ctx, dir, bump)
		if err != nil {
			report.Held = fmt.Sprintf("the version of %s could not be raised: %v", report.Rig, err)
			return false
		}
		report.Version = bumped.Version
		report.versionCommit = bumped.Commit
		report.changelogFiles, report.changelogWritten = bump.Changelog, bump.Notes()
		report.writtenAfterFixer = report.Fixer != nil && report.Fixer.Started
		report.Notes = append(report.Notes, bumped.Tidied...)
	}
	return true
}

package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// namedShas reads the main shas a bug's description names: the 'Main's sha:'
// line and the 'main at <sha>' of its first line, as mw suite --file writes
// them (mw-gq6.993).
var namedShas = regexp.MustCompile(`(?i)(?:main's sha:|main at)\s+([0-9a-f]{7,40})\b`)

// namesSha says the bug's description names sha as the main it failed at. A
// short sha and a long one that begin alike are the same commit.
func namesSha(description, sha string) bool {
	sha = strings.ToLower(strings.TrimSpace(sha))
	if sha == "" {
		return false
	}
	for _, m := range namedShas.FindAllStringSubmatch(description, -1) {
		named := strings.ToLower(m[1])
		if strings.HasPrefix(sha, named) || strings.HasPrefix(named, sha) {
			return true
		}
	}
	return false
}

// settlePassedBug is what a dispatch does with a bug whose check passed on its
// rig's main. When main is the very sha the bug names, no story has landed since
// it failed, so nothing fixed it: it failed and passed at one commit, which is a
// flake, and the bug is held with that said, not closed as fixed. Otherwise main
// has moved and the bug is closed as already fixed (mw-gq6.414, mw-gq6.993). It
// returns what recheckBug does: fixed is true when the story is dealt with and
// is not to be dispatched.
func (d Dispatch) settlePassedBug(ctx context.Context, detail StoryDetail, rig string, check BugCheck, found SpecRecheck, report *DispatchReport) (said string, fixed bool) {
	id := detail.Story.ID
	if namesSha(detail.Description, found.Sha) {
		return d.holdFlakyBug(ctx, id, rig, check, found, report)
	}
	one := FixedOnMain{StoryID: id, Spec: check.String(), Sha: found.Sha, Tests: found.Tests}
	if err := d.closeStory(ctx, "closing a bug found fixed on main", id, one.reason()); err != nil {
		said := fmt.Sprintf("%s passes on main but the story could not be closed: %v", check, oneLine(err.Error()))
		d.noteRecheckSkip(ctx, report, id, said)
		return said, false
	}
	comment := fmt.Sprintf("mw dispatch on %s ran %s (%s) on %s's main before dispatching this bug, and it passes at %s, "+
		"so the bug was closed with no session (a story that landed after the bug was filed fixed it).\n\nThe end of the run:\n%s",
		d.Host, check, check.Source, rig, found.Sha, strings.TrimSpace(found.Tail))
	if err := d.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: it was closed as fixed on main, but the run's tail could not be commented on: %v", id, err))
	}
	report.FixedOnMain = append(report.FixedOnMain, one)
	return "", true
}

// holdFlakyBug holds a bug that failed at a sha and passed at the same sha, and
// says so on it. A hold that fails leaves the bug ready, and it is dispatched as
// a bug whose check passed but could not be settled.
func (d Dispatch) holdFlakyBug(ctx context.Context, id, rig string, check BugCheck, found SpecRecheck, report *DispatchReport) (said string, fixed bool) {
	note := fmt.Sprintf("%s mw dispatch on %s ran %s (%s) on %s's main and it passes at %s, the very commit this bug says it failed at: "+
		"no story has landed since, so nothing fixed it. It is flaky at that commit, not fixed, and it is held for the Mayor.\n\nThe end of the run:\n%s",
		HeldCommentPrefix, d.Host, check, check.Source, rig, found.Sha, strings.TrimSpace(found.Tail))
	if err := d.Tracker.HoldStory(ctx, id); err != nil {
		said := fmt.Sprintf("%s passes at the sha the bug names but the story could not be held: %v", check, oneLine(err.Error()))
		d.noteRecheckSkip(ctx, report, id, said)
		return said, false
	}
	if err := d.Tracker.CommentOnStory(ctx, id, note); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: it was held as flaky at %s, but the comment could not be written: %v", id, short(found.Sha), err))
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s: %s passes at %s, the sha the bug names; flaky, not fixed: held", id, check, short(found.Sha)))
	return "", true
}

package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
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
//
// A bug mw next filed from a Builder's finding (LabelFoundAtClose) names no sha,
// and its check may already have passed when it was filed (a finder's command
// that was not read to fail at its own commit is filed all the same), so a pass
// on main shows no story fixed it: it is held for the Mayor, not closed
// (mw-gq6.1737).
func (d Dispatch) settlePassedBug(ctx context.Context, detail StoryDetail, rig string, check BugCheck, found SpecRecheck, report *DispatchReport) (said string, fixed bool) {
	id := detail.Story.ID
	if namesSha(detail.Description, found.Sha) {
		return d.holdFlakyBug(ctx, id, rig, check, found, report)
	}
	if slices.Contains(detail.Labels, LabelFoundAtClose) {
		return d.holdFoundBug(ctx, id, rig, check, found, report)
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

// holdFoundBug holds a bug filed from a finding whose check passes on main, and
// says so on it: nothing shows the check failed when the bug was filed, so no
// story is known to have fixed it. A hold that fails leaves the bug ready, and it
// is dispatched as a bug whose check passed but could not be settled.
func (d Dispatch) holdFoundBug(ctx context.Context, id, rig string, check BugCheck, found SpecRecheck, report *DispatchReport) (said string, fixed bool) {
	note := fmt.Sprintf("%s mw dispatch on %s ran %s (%s) on %s's main and it passes at %s, but this bug was filed from a Builder's finding and names no commit it failed at: "+
		"nothing shows the check failed when the bug was filed, so no story is known to have fixed it. It is held for the Mayor, not closed as fixed.\n\nThe end of the run:\n%s",
		HeldCommentPrefix, d.Host, check, check.Source, rig, found.Sha, strings.TrimSpace(found.Tail))
	if err := d.Tracker.HoldStory(ctx, id); err != nil {
		said := fmt.Sprintf("%s passes on main but the story could not be held: %v", check, oneLine(err.Error()))
		d.noteRecheckSkip(ctx, report, id, said)
		return said, false
	}
	if err := d.Tracker.CommentOnStory(ctx, id, note); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: it was held as a finding's bug that passes on main, but the comment could not be written: %v", id, err))
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s: %s passes at %s, but the bug was filed from a finding and is not known to have failed: held, not closed", id, check, short(found.Sha)))
	return "", true
}

// recheckBug re-runs the check a bug names on its rig's main, before the story is
// claimed (mw-gq6.414, mw-gq6.775). A check that passes closes the story (no
// session, no claim, no worktree) and fixed is true. Otherwise the story is
// dispatched as it always was, and said is what the re-run came to for its line:
// that the check still fails, why it could not be run, or that the bug names none.
// Nothing is re-run in a dry run, and nothing for a story that is not a bug, or is
// a flaky file's bug.
//
// The story is read again right before the re-run and again before anything is
// written to it (mw-gq6.1629): a re-run takes minutes, and another host's dispatch
// may have claimed the bug since the ready listing. A bug claimed by then is not
// re-run, and one claimed while it ran gets a comment and no close.
func (d Dispatch) recheckBug(ctx context.Context, detail StoryDetail, path domain.Path, report *DispatchReport) (said string, fixed bool) {
	check, named, asked := bugCheck(detail, path.Files)
	if (d.Rechecks == nil && d.ExampleRechecks == nil) || d.DryRun || !asked {
		return "", false
	}
	id := detail.Story.ID
	if !named {
		return noCheckNamed, false
	}
	if who := d.heldElsewhere(ctx, id); who != "" {
		report.Passed = append(report.Passed, Passed{StoryID: id, Gate: ClaimLostGate,
			Why: fmt.Sprintf("%s was claimed by %s before its re-run on main, so it was not re-run", id, who)})
		return "", true
	}
	var found SpecRecheck
	var err error
	if check.IsExample() {
		if d.ExampleRechecks == nil {
			return "", false
		}
		found, err = d.ExampleRechecks.RecheckExamples(ctx, check.App, check.Kind, check.Examples)
		if errors.Is(err, ErrNoGristKey) {
			return noGristKeyHere, false
		}
	} else {
		if d.Rechecks == nil {
			return "", false
		}
		found, err = d.recheckUnderGate(ctx, id, path.Rig, check.Spec, report)
	}
	var busy gateBusy
	if errors.As(err, &busy) {
		d.noteRecheckSkip(ctx, report, id, busy.Error())
		return busy.Error(), false
	}
	switch {
	case err != nil:
		d.noteRecheckSkip(ctx, report, id, "not re-run on main first: "+firstLine(err.Error()))
		return fmt.Sprintf("not re-run on main first: %v", oneLine(err.Error())), false
	case !found.Passed:
		return fmt.Sprintf("re-run on main first: %s (%s) still fails at %s", check, check.Source, short(found.Sha)), false
	}
	if who := d.heldElsewhere(ctx, id); who != "" {
		d.commentFoundPassing(ctx, id, who, path.Rig, check, found, report)
		return "", true
	}
	return d.settlePassedBug(ctx, detail, path.Rig, check, found, report)
}

// heldElsewhere says who holds the story now, read fresh, or is empty when nobody
// does: it is claimed (by another host's dispatch, as this one claims only after
// its re-run), closed, or held by the Mayor. A read that fails says nobody holds
// it, so the dispatch goes on as it did before the read was made.
func (d Dispatch) heldElsewhere(ctx context.Context, id string) string {
	fresh, err := d.Tracker.ShowStory(ctx, id)
	if err != nil {
		return ""
	}
	switch {
	case strings.TrimSpace(fresh.Assignee) != "":
		return strings.TrimSpace(fresh.Assignee)
	case fresh.Closed():
		return "a close made elsewhere"
	case fresh.Held():
		return "the Mayor's hold"
	}
	return ""
}

// commentFoundPassing is all a dispatch writes to a bug someone else holds whose
// re-run passed on main: a comment for whoever holds it, never a close, a hold or
// a mail (mw-gq6.1629).
func (d Dispatch) commentFoundPassing(ctx context.Context, id, who, rig string, check BugCheck, found SpecRecheck, report *DispatchReport) {
	comment := fmt.Sprintf("found passing on main at %s by %s: %s (%s) passes on %s's main, but this bug is held by %s, so mw dispatch closed nothing.\n\nThe end of the run:\n%s",
		short(found.Sha), d.Host, check, check.Source, rig, who, strings.TrimSpace(found.Tail))
	if err := d.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: found passing on main, but held by %s, and the comment could not be written: %v", id, who, err))
		return
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s: %s passes on main at %s, but it is held by %s: commented, not closed", id, check, short(found.Sha), who))
}

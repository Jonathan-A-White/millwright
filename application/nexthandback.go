package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// CloseOutAgain reports whether a story was handed back by its close-out for
// its close-out alone (RunCloseOutAgain), as the run label on the story says:
// its merge slot was still held at the overall most a close-out waits, its
// claim, worktree and branch are kept, and it has no session until the next
// dispatch tick runs its close-out again. Read off the listing, as
// QueuedForBatch is, so that whatever walks the claimed stories can pass it
// over without asking the tracker again.
func CloseOutAgain(detail StoryDetail) bool {
	return hasLabel(detail.Labels, RunState+":"+RunCloseOutAgain)
}

// handBack is what a close-out does when its rig's merge slot was still held at
// the overall most it waits (ErrSlotCap, mw-gq6.467), in place of stopping: the
// session finished and its work is committed, so nothing about the story is at
// fault and nothing waits on the Mayor. The claim, the worktree and the branch
// are kept; run=close-out-again is recorded, with a comment and a ledger line
// saying why; and the next dispatch tick runs the close-out alone again, from
// the committed branch, never a fresh Builder session. Nothing is merged,
// pushed, mailed or dispatched.
func (n Next) handBack(ctx context.Context, c *closeOut, report *NextReport, why string) (NextReport, error) {
	if ctx.Err() != nil {
		return n.interrupted(ctx, c, report)
	}
	report.Why, report.Reason, report.HandedBack = why, ReasonMergeSlot, true

	note := fmt.Sprintf("mw next on %s handed %s back (%s): %s\n\n"+
		"Nothing was merged and nothing was pushed, and nothing is wrong with the work: the claim, the worktree %s and the branch %s are kept, "+
		"and the next dispatch tick runs its close-out alone again from %s, not a new Builder session.",
		n.Host, c.id, ReasonMergeSlot, why, c.worktree, c.branch, c.branch)
	// The run state first: it is what tells the dispatch, the sweep and another
	// close-out that the story waits for its close-out, not for a person.
	n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunCloseOutAgain),
		func() error {
			return n.Tracker.SetStoryState(ctx, c.id, RunState, RunCloseOutAgain, "("+string(ReasonMergeSlot)+") "+why)
		})
	n.writeOrSay(ctx, c, report, "the hand-back on the story", fmt.Sprintf("the hand-back could not be written on %s", c.id),
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, note) })
	if err := n.ledger(ctx, c, report, NotLanded(ReasonMergeSlot, firstLine(why)+"; handed back for its close-out alone")); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)
	return *report, fmt.Errorf("closing out %s: %s; handed back, and the next dispatch tick runs its close-out alone again", c.id, why)
}

// alreadyFixedOnMain is what a Builder writes in its closing comment when it found
// the bug fixed on the target branch before it began.
var alreadyFixedOnMain = regexp.MustCompile(`(?i)\balready\s+fixed\s+on\s+(?:the\s+)?(?:main|master|target)\b`)

// closeAsFixedOnMain is what a close-out does with a bug whose Builder committed
// nothing and wrote a closing comment saying it is fixed on main already
// (mw-gq6.869): it runs the story's spec on the rig's main, as dispatch does before
// a claim, and when it passes the story is closed as fixed, its clean worktree and
// its branch with no commits of its own are taken away, and nobody is asked. closed
// is false for anything else, and the refusal stands as it was; a re-run that
// fails or could not be made is a note. A grist example or any other check is not
// run here, nor is a story that is not a bug.
func (n Next) closeAsFixedOnMain(ctx context.Context, c *closeOut, report *NextReport, refusal Refusal) (NextReport, bool, error) {
	if refusal.Reason != ReasonNoCommits || n.Rechecks == nil || ctx.Err() != nil {
		return NextReport{}, false, nil
	}
	check, named, asked := bugCheck(c.detail, c.path.Files)
	if !asked || !named || check.IsExample() {
		return NextReport{}, false, nil
	}
	text, written := n.closingCommentTextOnStory(ctx, c, StartPoint(n.remote(), c.target))
	if !written || !alreadyFixedOnMain.MatchString(text) {
		return NextReport{}, false, nil
	}
	found, err := rerunUnderGate(ctx, n.Rechecks, n.GateLock, DispatchGateWait, n.Host, c.id, c.path.Rig, check.Spec, func(note string) {
		report.Notes = append(report.Notes, note)
	})
	switch {
	case err != nil:
		report.Notes = append(report.Notes, fmt.Sprintf("%s says the bug is fixed on %s, but its check was not re-run there: %s", c.id, c.target, firstLine(err.Error())))
		return NextReport{}, false, nil
	case !found.Passed:
		report.Notes = append(report.Notes, fmt.Sprintf("%s says the bug is fixed on %s, but %s still fails there at %s", c.id, c.target, check, short(found.Sha)))
		return NextReport{}, false, nil
	}

	one := FixedOnMain{StoryID: c.id, Spec: check.String(), Sha: found.Sha, Tests: found.Tests}
	report.FixedOnMain = &one
	outcome := one.reason()
	comment := fmt.Sprintf("mw next on %s ran %s (%s) on %s's %s after the Builder committed nothing and said the bug is fixed there already, "+
		"and it passes at %s, so the story is closed as fixed with nothing to land. Its worktree %s and its branch %s had no work of their own and are taken away.\n\nThe end of the run:\n%s",
		n.Host, check, check.Source, c.path.Rig, c.target, found.Sha, c.worktree, c.branch, strings.TrimSpace(found.Tail))
	n.writeOrSay(ctx, c, report, "the fixed-on-main comment", fmt.Sprintf("the comment could not be written on %s", c.id),
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, comment) })
	if err := n.Worktrees.Remove(ctx, c.rigDir, c.worktree, c.branch); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the worktree %s could not be taken away: %v", c.worktree, err))
	}
	if err := n.ledger(ctx, c, report, outcome); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)
	notes, closeErr := n.closeStory(ctx, c.id, outcome)
	report.Notes = append(report.Notes, notes...)
	if closeErr != nil {
		report.NotClosed = closeErr.Error()
		return *report, true, fmt.Errorf("closing out %s: %s passes on %s, but the story could not be closed, so it is still open: %w", c.id, check, c.target, closeErr)
	}
	n.closeFormula(ctx, c, report, "fixed on "+c.target)
	report.Closed = true
	rep, err := n.carryOn(ctx, c, report)
	return rep, true, err
}

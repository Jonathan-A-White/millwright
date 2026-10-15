package application

import (
	"context"
	"fmt"
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

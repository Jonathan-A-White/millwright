package application

import (
	"context"
	"fmt"
)

// handBackRedAlone answers a story that a batch of it alone has held red for
// the second time since the rig last landed a batch. The first hold left it out
// of every batch that had other stories in it (leaveOutHeldAlone), but with
// nothing else queued it was taken again, held red again and mailed again, for
// as long as nothing else was queued, and nothing handed it back
// (mw-gq6.1631, mw-gq6.1634). Its tests are red with nothing else in the batch,
// which a Builder mends on its kept branch: the run state is cleared and the
// claim given back, as handBackRepeaters does, and a comment says why.
//
// A story that cannot be handed back is left queued, and the report says why.
func (b BatchLand) handBackRedAlone(ctx context.Context, report *BatchReport) {
	n := b.Next
	if b.Metrics == nil || n.Tracker == nil || report.Landed || report.RedAt.IsZero() || len(report.Queue) != 1 {
		return
	}
	s := report.Queue[0]
	before, again := HeldRedAloneBefore(rigBatches(readBatches(ctx, b.Metrics), report.Rig), s.ID)
	if !again {
		return
	}
	why, left := b.giveBack(ctx, s, report)
	if left {
		return
	}
	if why != "" {
		report.Notes = append(report.Notes, fmt.Sprintf("%s was held red alone for the second time, but it could not be handed back to a Builder, so it stays queued: %s", s.ID, why))
		return
	}
	comment := fmt.Sprintf("Handed back by %s to a Builder: the rig's tests were red on this story alone, in %s and again in this batch, with nothing else in the batch to blame.\n"+
		"Its branch %s is kept at the origin. Run the suite on it as it stands on %s, mend what is red and commit. "+
		"Its run state %s is cleared and its claim given back, so the next dispatch tick takes it as a new attempt, and once its work is committed `mw next` queues it for the next batch again.",
		report.Label(), BatchLabel(report.Rig, before), StartPoint(n.remote(), StoryBranch(s.ID)), StartPoint(n.remote(), report.Target), RunState+":"+RunQueued)
	if err := writeRetrying(ctx, n, nil, "the hand-back note", func() error { return n.Tracker.CommentOnStory(ctx, s.ID, comment) }); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s was handed back, but the comment saying why could not be written on it: %v", s.ID, err))
	}
	report.RedAloneBack = append(report.RedAloneBack, s)
	report.Notes = append(report.Notes, fmt.Sprintf("%s was handed back to a Builder: it was held red alone in %s and again in %s", s.ID, BatchLabel(report.Rig, before), report.Label()))
}

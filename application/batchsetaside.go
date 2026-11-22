package application

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SetAsidePrefix starts the comment a story taken out of the batch queue by
// hand carries, with the why the Mayor gave.
const SetAsidePrefix = "SET-ASIDE:"

// The comment opens HELD: as every hold's reason does, so that mw stuck leaves
// the story alone (HeldCommentPrefix).

// SetAside is mw land <rig> --set-aside <story> --why "...": it takes one story
// out of the rig's batch queue by hand, so that the next mw land takes the
// rest as one batch (mw-gq6.1541). The story's branch is kept, its claim is
// given back held, and a comment carries the why. Its run state stays queued,
// so that mw release puts it back in the queue.
type SetAside struct {
	Tracker WorkTracker
	Host    string
	Out     io.Writer
}

// Run sets story id of rig aside for why. A story of another rig, one not queued
// for batch landing, or one with no why is refused and left as it was.
func (s SetAside) Run(ctx context.Context, rig, id, why string) error {
	why = strings.TrimSpace(why)
	if why == "" {
		return fmt.Errorf("setting %s aside: say why, with --why \"...\": the comment on the story carries it", id)
	}
	detail, err := s.Tracker.ShowStory(ctx, id)
	if err != nil {
		return fmt.Errorf("setting %s aside: %w", id, err)
	}
	switch path, perr := detail.Path(); {
	case perr != nil:
		return fmt.Errorf("setting %s aside: its path cannot be read: %w", id, perr)
	case path.Rig != rig:
		return fmt.Errorf("setting %s aside: it is a story of %s, not of %s", id, path.Rig, rig)
	}
	if !QueuedForBatch(detail) {
		return fmt.Errorf("setting %s aside: it is not queued for batch landing (status %s)", id, detail.Status)
	}
	if strings.EqualFold(strings.TrimSpace(detail.Status), StatusHeld) {
		return fmt.Errorf("setting %s aside: it is held already: mw release %s puts it back in the queue", id, id)
	}
	if err := givenBackAs(ctx, s.Tracker, id, StatusHeld, holdQueued(ctx, s.Tracker, id, detail.Assignee)); err != nil {
		return fmt.Errorf("setting %s aside: its claim could not be given back held: %w", id, err)
	}
	comment := fmt.Sprintf(HeldCommentPrefix+" %s %s\nmw land %s --set-aside on %s took it out of the batch queue. Its branch %s is kept at the origin, its claim was given back and the story is held: the next batch is made of the rest. mw release %s puts it back in the queue.",
		SetAsidePrefix, why, rig, s.Host, StoryBranch(id), id)
	if err := s.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		return fmt.Errorf("setting %s aside: it is held, but the comment saying why could not be written: %w", id, err)
	}
	if s.Out != nil {
		fmt.Fprintf(s.Out, "%s is set aside: held, its branch %s kept; the next mw land %s makes its batch of the rest. mw release %s puts it back.\n", id, StoryBranch(id), rig, id)
	}
	return nil
}

// holdQueued holds story id whoever holds its claim: a claim held by holder is
// given back held, guarded on holder, as another host's claim is. A story nobody
// holds has no claim to give back, and an empty holder would be read as this
// host's actor, so bd would refuse a guard naming one that is not there
// (mw-gq6.1661): it is held with HoldStory, which asks for the story to be
// unassigned.
func holdQueued(ctx context.Context, tracker WorkTracker, id, holder string) error {
	if holder == "" {
		return tracker.HoldStory(ctx, id)
	}
	return tracker.HoldClaimHeldBy(ctx, id, holder)
}

// countConflictSetAside is how many batches have set story id aside for a
// conflict, the one that is setting it aside now included (BatchConflictsField).
// It is read off the story itself, not the listing the batch began with, which
// an earlier batch of another host may have outdated; when it cannot be read the
// listing's is used, and the report says so.
func (b BatchLand) countConflictSetAside(ctx context.Context, id string, report *BatchReport) int {
	before := 0
	for _, s := range report.Queue {
		if s.ID == id {
			before = s.detail.BatchConflicts
		}
	}
	if detail, err := b.Next.Tracker.ShowStory(ctx, id); err == nil {
		before = detail.BatchConflicts
	} else {
		report.Notes = append(report.Notes, fmt.Sprintf("how many batches set %s aside before could not be read, so the listing's count of %d is used: %v", id, before, err))
	}
	return before + 1
}

// countSetAside counts this batch's setting story s aside, for whatever reason,
// on the story itself (BatchConflictsField), with the other metadata fields the
// reason has to record. A story the batches set aside again and again is handed
// back by handBackRepeaters, not queued for ever (mw-gq6.1631, mw-gq6.1632).
func (b BatchLand) countSetAside(ctx context.Context, s *BatchStory, report *BatchReport, fields map[string]string) {
	n := b.Next
	s.ConflictSetAsides = b.countConflictSetAside(ctx, s.ID, report)
	if fields == nil {
		fields = map[string]string{}
	}
	fields[BatchConflictsField] = strconv.Itoa(s.ConflictSetAsides)
	if werr := writeRetrying(ctx, n, nil, "the count of set-asides", func() error { return n.Tracker.SetStoryMetadata(ctx, s.ID, fields) }); werr != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("how many batches set %s aside, and the files it conflicted in, could not be recorded on it: %v", s.ID, werr))
	}
}

// setAsideSplit divides what the batch set aside into the stories that stay
// queued for the next batch and the ones handed back to a Builder to rebase.
func (r BatchReport) setAsideSplit() (queued, back []BatchStory) {
	for _, s := range r.SetAside {
		if s.HandedBack {
			back = append(back, s)
		} else {
			queued = append(queued, s)
		}
	}
	return queued, back
}

// handBackRepeaters answers a story set aside for a conflict with the batch for
// the MaxBatchConflicts'th time. Only a batch of which every branch conflicts is
// given its fixer (landIn), so a story that conflicts beside branches that merge
// is never given it: it stayed run=queued, the next batch took it, it conflicted
// again and was set aside again, for as long as other stories kept the batch from
// being all conflicts (mw-5r3p30.1231 and .1221, batches 622 and 623 of lampas,
// mw-gq6.1631). Its branch conflicts with the target branch as it now is, and
// what resolves that is a rebase on it, which is a Builder's: the run state is
// cleared and the claim given back, so that the next dispatch tick takes the story
// as a new attempt on its kept branch, and a comment names the files.
//
// A story that cannot be handed back is left queued, and the report says why.
func (b BatchLand) handBackRepeaters(ctx context.Context, report *BatchReport) {
	n := b.Next
	for i := range report.SetAside {
		s := &report.SetAside[i]
		if s.ConflictSetAsides < MaxBatchConflicts || n.Tracker == nil {
			continue
		}
		if why := b.handBack(ctx, *s, report); why != "" {
			report.Notes = append(report.Notes, fmt.Sprintf("%s was set aside for a conflict for the %s time, but it could not be handed back to a Builder to rebase, so it stays queued: %s",
				s.ID, ordinal(s.ConflictSetAsides), why))
			continue
		}
		s.HandedBack = true
		s.Why += fmt.Sprintf("; set aside for a conflict for the %s time, so it was handed back to a Builder to rebase on %s", ordinal(s.ConflictSetAsides), report.Target)
		report.Notes = append(report.Notes, fmt.Sprintf("%s was handed back to a Builder to rebase on %s: %s", s.ID, report.Target, ConflictedFilesLine(s.ConflictFiles)))
	}
}

// handBack takes story s off the batch queue, gives its claim back and says on
// it why; what it returns is why it could not, empty when it did.
func (b BatchLand) handBack(ctx context.Context, s BatchStory, report *BatchReport) string {
	n := b.Next
	if why := b.giveBack(ctx, s); why != "" {
		return why
	}
	var comment string
	if len(s.Conflicts) == 0 {
		comment = fmt.Sprintf("Handed back by %s to a Builder: this story was set aside by %d batches, for a reason other than a merge conflict (%s)\n"+
			"A batch takes a story's branch %s from the rig's origin and merges it as it stands, and could not. Mend what is said above (a branch not at the origin is pushed again, a commit signed as a machine's work is reworded without its Co-Authored-By trailer or Generated-with line), run the suite and commit. "+
			"Its run state %s is cleared and its claim given back, so the next dispatch tick takes it as a new attempt, and once its work is committed `mw next` queues it for the next batch again.",
			report.Label(), s.ConflictSetAsides, firstLine(s.Why), StartPoint(n.remote(), StoryBranch(s.ID)), RunState+":"+RunQueued)
	} else {
		files := "the files were not named by the merge"
		if line := ConflictedFilesLine(s.ConflictFiles); line != "" {
			files = line
		}
		comment = fmt.Sprintf("Handed back by %s to a Builder to rebase on its target branch: this story was set aside for a merge conflict by %d batches (%s)\n%s\n"+
			"Its branch %s does not merge into %s as it stands now, and a batch's fixer is given a batch only when every branch in it conflicts, so none was given this story beside branches that merged. "+
			"Rebase %s onto %s, resolve the conflicts above, run the suite and commit. Its run state %s is cleared and its claim given back, so the next dispatch tick takes it as a new attempt, and once its work is committed `mw next` queues it for the next batch again.",
			report.Label(), s.ConflictSetAsides, firstLine(s.Why), files, StoryBranch(s.ID), report.Target, StoryBranch(s.ID), StartPoint(n.remote(), report.Target), RunState+":"+RunQueued)
	}
	if err := writeRetrying(ctx, n, nil, "the hand-back note", func() error { return n.Tracker.CommentOnStory(ctx, s.ID, comment) }); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s was handed back, but the comment naming the conflicted files could not be written on it: %v", s.ID, err))
	}
	return ""
}

// giveBack takes story s off the batch queue: its run state is cleared and its
// claim given back, so that the next dispatch tick takes it as a new attempt;
// what it returns is why it could not, empty when it did.
func (b BatchLand) giveBack(ctx context.Context, s BatchStory) string {
	n := b.Next
	// The run state first, as the harness-stop give-back does it (mw-gq6.1382):
	// no open story is ever seen carrying run:queued.
	if err := writeRetrying(ctx, n, nil, "the clearing of the run state", func() error { return n.Tracker.ClearRunState(ctx, s.ID) }); err != nil {
		return fmt.Sprintf("its run state could not be cleared: %v", err)
	}
	holder := s.detail.Assignee
	if detail, err := n.Tracker.ShowStory(ctx, s.ID); err == nil {
		holder = detail.Assignee
	}
	if err := writeRetrying(ctx, n, nil, "the give-back of the claim on "+s.ID, func() error {
		return givenBackAs(ctx, n.Tracker, s.ID, StatusOpen, releaseQueued(ctx, n.Tracker, s.ID, holder))
	}); err != nil {
		// Queued again, so that it is not left claimed with no run state for the sweep to find.
		if serr := n.Tracker.SetStoryState(ctx, s.ID, RunState, RunQueued, "handing it back failed: "+firstLine(err.Error())); serr != nil {
			return fmt.Sprintf("its claim could not be given back: %v (and run=%s could not be written again: %v)", err, RunQueued, serr)
		}
		return fmt.Sprintf("its claim could not be given back: %v", err)
	}
	return ""
}

// releaseQueued gives back the claim of story id held by holder, guarded on
// holder. A story nobody holds has no claim to give back, and an empty holder
// would be read as this host's actor, so bd would refuse (mw-gq6.1661): nothing
// is written, and givenBackAs reads the story to say whether it is open and
// unclaimed.
func releaseQueued(ctx context.Context, tracker WorkTracker, id, holder string) error {
	if holder == "" {
		return nil
	}
	return tracker.ReleaseClaimHeldBy(ctx, id, holder)
}

// ordinal says a count as a word: "second", "third", "4th".
func ordinal(n int) string {
	switch n {
	case 2:
		return "second"
	case 3:
		return "third"
	}
	return fmt.Sprintf("%dth", n)
}

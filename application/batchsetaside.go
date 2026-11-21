package application

import (
	"context"
	"fmt"
	"io"
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
	holder := detail.Assignee
	if err := givenBackAs(ctx, s.Tracker, id, StatusHeld, s.Tracker.HoldClaimHeldBy(ctx, id, holder)); err != nil {
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

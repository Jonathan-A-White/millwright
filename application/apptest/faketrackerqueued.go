package apptest

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// ReleaseQueuedHeldBy implements application.WorkTracker: the run state cleared
// and the claim given back in the one write, conditional on holder, which an
// empty holder makes nobody, and on the claim itself: a story whose latest claim
// was made at some other time than claimedAt (a zero claimedAt asks for no such
// check) was given back and taken again, as the same actor, since it was read.
func (f *FakeTracker) ReleaseQueuedHeldBy(_ context.Context, id, holder string, claimedAt time.Time) error {
	f.note("ReleaseQueuedHeldBy")
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Assignee != holder {
			return fmt.Errorf("release %s: held by %q, not %q: %w", id, s.detail.Assignee, holder, application.ErrStoryMovedOn)
		}
		if !claimedAt.IsZero() && !s.detail.ClaimedAt.Equal(claimedAt) {
			return fmt.Errorf("release %s: its claim was made at %s, not %s: %w", id, s.detail.ClaimedAt.Format(time.RFC3339), claimedAt.Format(time.RFC3339), application.ErrStoryMovedOn)
		}
		s.clearRunState()
		s.detail.Status = StatusOpen
		s.detail.Assignee = ""
		s.detail.LeaseExpires = time.Time{}
		return nil
	})
}

// CommentOnStoryIfQueued implements application.WorkTracker: the comment is
// written only on a story still labelled queued for batch landing, in the one
// write; a story that is not is left as it is and the error wraps
// application.ErrStoryMovedOn.
func (f *FakeTracker) CommentOnStoryIfQueued(ctx context.Context, id, text string) error {
	f.note("CommentOnStoryIfQueued")
	detail, err := f.ShowStory(ctx, id)
	if err == nil && !application.QueuedForBatch(detail) {
		return fmt.Errorf("comment on %s: it is no longer queued: %w", id, application.ErrStoryMovedOn)
	}
	return f.CommentOnStory(ctx, id, text)
}

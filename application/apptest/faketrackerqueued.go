package apptest

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// ReleaseQueuedHeldBy implements application.WorkTracker: the run state cleared
// and the claim given back in the one write, conditional on holder, which an
// empty holder makes nobody.
func (f *FakeTracker) ReleaseQueuedHeldBy(_ context.Context, id, holder string) error {
	f.note("ReleaseQueuedHeldBy")
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Assignee != holder {
			return fmt.Errorf("release %s: held by %q, not %q: %w", id, s.detail.Assignee, holder, application.ErrStoryMovedOn)
		}
		s.clearRunState()
		s.detail.Status = StatusOpen
		s.detail.Assignee = ""
		s.detail.LeaseExpires = time.Time{}
		return nil
	})
}

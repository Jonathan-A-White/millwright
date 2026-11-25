package apptest

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// ReleaseClaim implements application.WorkTracker. It is conditional on the
// fake's own Actor, mirroring the real gateway's --if-assignee guard: a claim
// another actor holds — set up with ClaimAs — is left exactly as it is.
func (f *FakeTracker) ReleaseClaim(ctx context.Context, id string) error {
	return f.release(ctx, id, f.me(), time.Time{}, "ReleaseClaim")
}

// ReleaseClaimHeldBy implements application.WorkTracker. It is conditional on
// the holder named, not the fake's own Actor; an empty holder is the Actor. It is
// conditional on the claim too, as ReleaseQueuedHeldBy is: a story whose latest
// claim was made at some other time than claimedAt (zero asks for no check) was
// given back and taken again since it was read, and the error wraps
// application.ErrStoryMovedOn.
func (f *FakeTracker) ReleaseClaimHeldBy(ctx context.Context, id, holder string, claimedAt time.Time) error {
	if holder == "" {
		holder = f.me()
	}
	return f.release(ctx, id, holder, claimedAt, "ReleaseClaimHeldBy")
}

// HoldClaimHeldBy implements application.WorkTracker: ReleaseClaimHeldBy that
// leaves the story held, not open, in the one write.
func (f *FakeTracker) HoldClaimHeldBy(ctx context.Context, id, holder string, claimedAt time.Time) error {
	if holder == "" {
		holder = f.me()
	}
	return f.release(ctx, id, holder, claimedAt, "HoldClaimHeldBy")
}

func (f *FakeTracker) release(_ context.Context, id, holder string, claimedAt time.Time, noted string) error {
	f.note(noted)
	f.mu.Lock()
	var fault *ReleaseFault
	if len(f.releaseFaults) > 0 {
		fault, f.releaseFaults = &f.releaseFaults[0], f.releaseFaults[1:]
	}
	f.mu.Unlock()
	if fault != nil {
		if fault.Err != nil {
			return fault.Err
		}
		// Lost: bd said it was done and the story did not change, as a write on
		// a Dolt connection that dropped can be (mw-gq6.372).
		return nil
	}
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Assignee != holder {
			return fmt.Errorf("release %s: held by %q, not %s", id, s.detail.Assignee, holder)
		}
		if !claimedAt.IsZero() && !s.detail.ClaimedAt.Equal(claimedAt) {
			return fmt.Errorf("release %s: its claim was made at %s, not %s: %w", id, s.detail.ClaimedAt.Format(time.RFC3339), claimedAt.Format(time.RFC3339), application.ErrStoryMovedOn)
		}
		if noted == "HoldClaimHeldBy" {
			s.detail.Status = StatusDeferred
		} else {
			// bd update --status open sets a story open whatever it was, a
			// closed one too (mw-gq6.854).
			s.detail.Status = StatusOpen
		}
		s.detail.Assignee = ""
		s.detail.LeaseExpires = time.Time{}
		return nil
	})
}

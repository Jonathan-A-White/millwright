package application_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1739: countSetAside still wrote the set-aside count and the conflicted
// files onto a story that mw retry gave a new attempt during the merge, though
// the batch then left the story alone (mw-gq6.1735). A batch that finds the
// story no longer queued when it comes to count it writes nothing on it.
func TestABatchDoesNotCountASetAsideOnARetriedStory(t *testing.T) {
	const id = "mw-e.2"
	for _, tc := range []struct {
		name  string
		batch func(t *testing.T) aSetAsideBatch
		count string
	}{
		{name: "the first time", count: "", batch: func(t *testing.T) aSetAsideBatch {
			a := aBatchOf(t, 3, id)
			a.landing.onlyBesideMates(id)
			return aQueuedClaim(t, a)
		}},
		{name: "the second time", count: "1", batch: func(t *testing.T) aSetAsideBatch {
			a := aBatchOf(t, 3, id)
			a.landing.onlyBesideMates(id)
			a = aQueuedClaim(t, a)
			a.run(t)
			return aLaterBatch(a, 596)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.batch(t)
			filesBefore := a.tracker.Metadata(id)[application.ConflictFilesField]
			a.land.Next.Landing = aRetryDuringTheMerge{aConflictInFiles{a.landing}, func(ctx context.Context) {
				if err := a.tracker.ClearRunState(ctx, id); err != nil {
					t.Fatal(err)
				}
				if err := a.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"); err != nil {
					t.Fatal(err)
				}
			}}
			a.run(t)

			meta := a.tracker.Metadata(id)
			if got := meta[application.BatchConflictsField]; got != tc.count {
				t.Errorf("expected the set-aside count of %s left at %q, got %q", id, tc.count, got)
			}
			if got := meta[application.ConflictFilesField]; got != filesBefore {
				t.Errorf("expected the conflicted files of %s left at %q, got %q", id, filesBefore, got)
			}
		})
	}
}

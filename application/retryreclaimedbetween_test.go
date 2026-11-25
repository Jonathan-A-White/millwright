package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1751: mw retry reads the story, and gives its claim back some calls
// later. A claim given back and taken again by the same actor in between (a
// same-host retry's new attempt is claimed by the one actor the host has) is
// not the claim the retry read, and its give-back must leave it alone.

// aReclaimBeforeTheGiveBack is the tracker, with another mw retry giving the
// claim back and this host's dispatch taking it again, as the same actor, in the
// instant before the retry's own give-back is written.
type aReclaimBeforeTheGiveBack struct {
	application.WorkTracker
	id        string
	reclaimed func(context.Context)

	done bool
}

func (t *aReclaimBeforeTheGiveBack) reclaim(ctx context.Context, id string) {
	if id == t.id && !t.done {
		t.done = true
		t.reclaimed(ctx)
	}
}

func (t *aReclaimBeforeTheGiveBack) ReleaseClaimHeldBy(ctx context.Context, id, holder string, claimedAt time.Time) error {
	t.reclaim(ctx, id)
	return t.WorkTracker.ReleaseClaimHeldBy(ctx, id, holder, claimedAt)
}

func (t *aReclaimBeforeTheGiveBack) HoldClaimHeldBy(ctx context.Context, id, holder string, claimedAt time.Time) error {
	t.reclaim(ctx, id)
	return t.WorkTracker.HoldClaimHeldBy(ctx, id, holder, claimedAt)
}

func TestRetryLeavesAClaimGivenBackAndTakenAgainBySameActorBetweenItsReadAndItsGiveBack(t *testing.T) {
	for _, hold := range []bool{false, true} {
		name := "open"
		if hold {
			name = "hold"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const id = "mw-gq6.1"
			tracker := apptest.NewFakeTracker()
			aRetryStory(t, tracker, id)
			first := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
			claimedAt := func(at time.Time) map[string]string {
				return map[string]string{application.ClaimedAtField: at.Format(time.RFC3339)}
			}
			if err := tracker.SetStoryMetadata(ctx, id, claimedAt(first)); err != nil {
				t.Fatal(err)
			}
			read, err := tracker.ShowStory(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := &aReclaimBeforeTheGiveBack{WorkTracker: tracker, id: id, reclaimed: func(ctx context.Context) {
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				must(tracker.ClearRunState(ctx, id))
				must(tracker.ReleaseClaimHeldBy(ctx, id, read.Assignee, time.Time{}))
				must(tracker.ClaimStory(ctx, id)) // the same actor, as the host's dispatch is
				must(tracker.SetStoryMetadata(ctx, id, claimedAt(first.Add(time.Hour))))
				must(tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"))
			}}

			retry := aRetry(tracker, &fakeRetryLanding{NoWorktree: true}, &apptest.FakeVaultFiles{}, &strings.Builder{})
			retry.Tracker = wrapped
			retry.Hold = hold
			if _, err := retry.Run(ctx, id); err == nil {
				t.Errorf("expected the retry to say its give-back was refused, got no error")
			}

			if !wrapped.done {
				t.Fatalf("the new attempt never landed between the read and the give-back: the test does not drive what it names")
			}
			if got := tracker.State(id, application.RunState); got != application.RunRunning {
				t.Errorf("expected the new attempt's run state %q left on %s, got %q", application.RunRunning, id, got)
			}
			detail, err := tracker.ShowStory(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if detail.Assignee != read.Assignee || detail.Status != application.StatusInProgress {
				t.Errorf("expected the new attempt's claim left on %s, got status %q held by %q", id, detail.Status, detail.Assignee)
			}
		})
	}
}

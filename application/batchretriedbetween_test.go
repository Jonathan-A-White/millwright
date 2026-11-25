package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1738: the batch re-reads a story before it acts on it (mw-gq6.1735),
// but the read and the write that follows it are two calls, and mw retry can
// land between them. The give-back that follows must be conditional on the
// attempt it read: a story that has changed hands is left alone, not stripped of
// the new attempt's run state and claim.

// aRetryAfterTheReread is the tracker, with mw retry giving the story a new
// attempt, claimed by another actor, in the instant after the batch has re-read
// it as queued: the first read of the story that follows the batch's writing of
// its count of set-asides.
type aRetryAfterTheReread struct {
	application.WorkTracker
	id      string
	retried func(context.Context)

	counted bool
	done    bool
}

func (t *aRetryAfterTheReread) SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error {
	if id == t.id {
		t.counted = true
	}
	return t.WorkTracker.SetStoryMetadata(ctx, id, fields)
}

func (t *aRetryAfterTheReread) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	detail, err := t.WorkTracker.ShowStory(ctx, id)
	if id == t.id && t.counted && !t.done {
		t.done = true
		t.retried(ctx)
	}
	return detail, err
}

func TestABatchLeavesAloneAStoryRetriedWhileItRanBetween(t *testing.T) {
	const id = "mw-e.2"
	a := aBatchOf(t, 3, id)
	a.landing.onlyBesideMates(id)
	a = aQueuedClaim(t, a)
	a.run(t)
	a = aLaterBatch(a, 596)

	ctx := context.Background()
	before := len(a.tracker.Comments(id))
	holder := ""
	if detail, err := a.tracker.ShowStory(ctx, id); err != nil {
		t.Fatal(err)
	} else {
		holder = detail.Assignee
	}
	wrapped := &aRetryAfterTheReread{WorkTracker: a.tracker, id: id, retried: func(ctx context.Context) {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(a.tracker.ClearRunState(ctx, id))
		must(a.tracker.ReleaseClaimHeldBy(ctx, id, holder, time.Time{}))
		must(a.tracker.ClaimAs(id, "mw@elsewhere", time.Now().Add(time.Hour)))
		must(a.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"))
	}}
	a.land.Next.Tracker = wrapped
	a.run(t)

	if !wrapped.done {
		t.Fatalf("the retry never landed between the re-read and the write: the test does not drive what it names")
	}
	if got := a.tracker.State(id, application.RunState); got != application.RunRunning {
		t.Errorf("expected the new attempt's run state %q left on %s, got %q", application.RunRunning, id, got)
	}
	detail, err := a.tracker.ShowStory(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "mw@elsewhere" {
		t.Errorf("expected the new attempt's claim left on %s, got held by %q (status %q)", id, detail.Assignee, detail.Status)
	}
	if got := a.tracker.Comments(id); len(got) != before {
		t.Errorf("expected no comment written on %s by the batch, got %q", id, got[before:])
	}
	if joined := strings.Join(a.report.Notes, "\n"); strings.Contains(joined, id+" was handed back") {
		t.Errorf("expected no note that %s was handed back, got %q", id, a.report.Notes)
	}
	for _, s := range a.report.SetAside {
		if s.ID == id && s.HandedBack {
			t.Errorf("expected %s not marked handed back", id)
		}
	}
}

// mw-gq6.1743: the guard on the holder alone does not see a claim given back and
// taken again by the same actor: a same-host retry hands the story to the next
// dispatch tick, which claims it as the one actor this host has. The give-back is
// conditional on the claim it read too (ClaimedAt, which every claim records).

// aRetryAndReclaimBeforeTheWrite is the tracker, with mw retry giving the
// story's claim back and the host's own dispatch taking it again as the same
// actor, in the instant after the batch has read the story and before its one
// write of the give-back.
type aRetryAndReclaimBeforeTheWrite struct {
	application.WorkTracker
	id      string
	retried func(context.Context)

	done bool
}

func (t *aRetryAndReclaimBeforeTheWrite) ReleaseQueuedHeldBy(ctx context.Context, id, holder string, claimedAt time.Time) error {
	if id == t.id && !t.done {
		t.done = true
		t.retried(ctx)
	}
	return t.WorkTracker.ReleaseQueuedHeldBy(ctx, id, holder, claimedAt)
}

func TestABatchLeavesAloneAStoryRetriedAndReclaimedBySameActor(t *testing.T) {
	const id = "mw-e.2"
	a := aBatchOf(t, 3, id)
	a.landing.onlyBesideMates(id)
	a = aQueuedClaim(t, a)
	ctx := context.Background()
	claimedAt := func(at time.Time) map[string]string {
		return map[string]string{application.ClaimedAtField: at.Format(time.RFC3339)}
	}
	first := time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)
	if err := a.tracker.SetStoryMetadata(ctx, id, claimedAt(first)); err != nil {
		t.Fatal(err)
	}
	a.run(t)
	a = aLaterBatch(a, 596)

	before := len(a.tracker.Comments(id))
	holder := ""
	if detail, err := a.tracker.ShowStory(ctx, id); err != nil {
		t.Fatal(err)
	} else {
		holder = detail.Assignee
	}
	wrapped := &aRetryAndReclaimBeforeTheWrite{WorkTracker: a.tracker, id: id, retried: func(ctx context.Context) {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(a.tracker.ClearRunState(ctx, id))
		must(a.tracker.ReleaseClaimHeldBy(ctx, id, holder, time.Time{}))
		must(a.tracker.ClaimStory(ctx, id)) // the same actor, as the host's dispatch is
		must(a.tracker.SetStoryMetadata(ctx, id, claimedAt(first.Add(time.Hour))))
		must(a.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"))
	}}
	a.land.Next.Tracker = wrapped
	a.run(t)

	if !wrapped.done {
		t.Fatalf("the retry never landed between the read and the write: the test does not drive what it names")
	}
	if got := a.tracker.State(id, application.RunState); got != application.RunRunning {
		t.Errorf("expected the new attempt's run state %q left on %s, got %q", application.RunRunning, id, got)
	}
	detail, err := a.tracker.ShowStory(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != holder || detail.Status != application.StatusInProgress {
		t.Errorf("expected the new attempt's claim left on %s, got status %q held by %q", id, detail.Status, detail.Assignee)
	}
	if got := a.tracker.Comments(id); len(got) != before {
		t.Errorf("expected no comment written on %s by the batch, got %q", id, got[before:])
	}
	if joined := strings.Join(a.report.Notes, "\n"); strings.Contains(joined, id+" was handed back") {
		t.Errorf("expected no note that %s was handed back, got %q", id, a.report.Notes)
	}
}

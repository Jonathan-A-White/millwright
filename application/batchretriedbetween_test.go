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
		must(a.tracker.ReleaseClaimHeldBy(ctx, id, holder))
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

package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1744: the batch re-reads a set-aside story before it writes its "stays
// queued" comment (mw-gq6.1735), and mw retry can land between the read and the
// write. The comment is written only on a story still queued, in the one call.

// aRetryBeforeTheNote is the tracker, with mw retry giving the story a new
// attempt in the instant after the batch has read it as queued and before its one
// write of the "stays queued" comment, whichever call writes it.
type aRetryBeforeTheNote struct {
	application.WorkTracker
	id      string
	retried func(context.Context)

	done bool
}

func (t *aRetryBeforeTheNote) retry(ctx context.Context, id string) {
	if id == t.id && !t.done {
		t.done = true
		t.retried(ctx)
	}
}

func (t *aRetryBeforeTheNote) CommentOnStory(ctx context.Context, id, text string) error {
	t.retry(ctx, id)
	return t.WorkTracker.CommentOnStory(ctx, id, text)
}

func (t *aRetryBeforeTheNote) CommentOnStoryIfQueued(ctx context.Context, id, text string) error {
	t.retry(ctx, id)
	return t.WorkTracker.CommentOnStoryIfQueued(ctx, id, text)
}

func TestABatchLeavesAloneAStoryRetriedBetweenNoteSetAsideReadAndComment(t *testing.T) {
	const id = "mw-e.2"
	a := aBatchOf(t, 3, id)
	a.landing.onlyBesideMates(id)
	a = aQueuedClaim(t, a)

	before := len(a.tracker.Comments(id))
	wrapped := &aRetryBeforeTheNote{WorkTracker: a.tracker, id: id, retried: func(ctx context.Context) {
		if err := a.tracker.ClearRunState(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := a.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"); err != nil {
			t.Fatal(err)
		}
	}}
	a.land.Next.Tracker = wrapped
	a.run(t)

	if !wrapped.done {
		t.Fatalf("the retry never landed between the re-read and the comment: the test does not drive what it names")
	}
	if got := a.tracker.State(id, application.RunState); got != application.RunRunning {
		t.Errorf("expected the new attempt's run state %q left on %s, got %q", application.RunRunning, id, got)
	}
	if got := a.tracker.Comments(id); len(got) != before {
		t.Errorf("expected no comment written on %s by the batch, got %q", id, got[before:])
	}
	if joined := strings.Join(a.report.Notes, "\n"); strings.Contains(joined, id+" was set aside by") {
		t.Errorf("expected no note that %s stays queued, got %q", id, a.report.Notes)
	}
}

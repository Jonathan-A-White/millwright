package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1747: the follower's probe left a queued story whose path cannot be read
// out of its due-count and its queue ids with a bare continue, though the batch
// answers such a story (mw-gq6.1734: it hands it back and mails the Mayor). A
// rig whose only queued story had an unreadable path was never due, so no batch
// ran to answer it. The probe counts it where the batch will answer it: in the
// rig its own path names, or, naming none, in the first rig of this host.

// anUnreadablePathQueued puts a story under epic e-<epicRig> whose model no
// harness runs on, so that its path cannot be read, queued 600 minutes ago, with
// the own rig given (empty for none).
func anUnreadablePathQueued(t *testing.T, j *aLandingJob, epicRig, ownRig, id string) {
	t.Helper()
	ctx := context.Background()
	j.tracker.AddStory("e-"+epicRig, domain.Story{ID: id, Title: "The story " + id,
		Overrides: domain.Path{Rig: ownRig, Model: "no-such-model"}})
	mustDo(t, j.tracker.ClaimStory(ctx, id))
	queue(t, j.tracker, id)
	mustDo(t, j.tracker.SetStoryMetadata(ctx, id, map[string]string{
		application.QueuedAtField: j.now.Add(-600 * time.Minute).Format(time.RFC3339),
	}))
	detail, err := j.tracker.ShowStory(ctx, id)
	mustDo(t, err)
	if _, perr := detail.Path(); perr == nil {
		t.Fatal("the test does not drive what it names: the story's path can be read")
	}
}

func TestTheBatchLandProbeCountsAQueuedStoryWithAnUnreadablePathAsOneTheBatchWillAnswer(t *testing.T) {
	ctx := context.Background()

	t.Run("named by its own rig, listed from the work in hand", func(t *testing.T) {
		j := newLandingJob(t)
		j.rule("lampas", domain.LandingEach)
		anUnreadablePathQueued(t, j, "lampas", "lampas", "lp-1")
		reason, wanted := j.landingsOn(j.tracker).Job().Probe(ctx)
		if !wanted || !strings.Contains(reason, "lampas") || !strings.Contains(reason, "1 queued") {
			t.Fatalf("expected lampas due for its one queued story, got %q, %v", reason, wanted)
		}
	})

	t.Run("naming no rig, it goes to the first rig of the host", func(t *testing.T) {
		j := newLandingJob(t)
		j.rule("lampas", domain.LandingEach)
		j.tracker.AddEpic("e-none", domain.Path{}) // no default rig either
		anUnreadablePathQueued(t, j, "none", "", "x-1")
		reason, wanted := j.landingsOn(j.tracker).Job().Probe(ctx)
		if !wanted || !strings.Contains(reason, "lampas") {
			t.Fatalf("expected lampas due for the stories no rig claims, got %q, %v", reason, wanted)
		}
	})

	t.Run("named by another rig, listed from the work in hand, it is not counted here", func(t *testing.T) {
		j := newLandingJob(t)
		j.rule("lampas", domain.LandingEach)
		anUnreadablePathQueued(t, j, "millwright", "millwright", "mw-1")
		if reason, wanted := j.landingsOn(j.tracker).Job().Probe(ctx); wanted {
			t.Fatalf("expected a story of a rig not on this host left alone, got %q", reason)
		}
	})

	t.Run("read by the queue read", func(t *testing.T) {
		j := newLandingJob(t)
		j.rule("lampas", domain.LandingEach)
		reader := &staticQueue{FakeTracker: j.tracker, stories: []application.QueuedStory{
			{ID: "lp-1", Rig: "lampas", Status: application.StatusInProgress},
			{ID: "lp-2", Rig: "", Status: application.StatusInProgress},
		}}
		reason, wanted := j.landingsOn(reader).Job().Probe(ctx)
		if !wanted || !strings.Contains(reason, "2 queued") {
			t.Fatalf("expected both stories counted, got %q, %v", reason, wanted)
		}
		if reader.works != 0 {
			t.Fatalf("expected no work in hand listed, got %d", reader.works)
		}
	})
}

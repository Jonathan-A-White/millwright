package application_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Over a link with a long round trip every read of a story is seconds, so a
// loop that reads what a story waits on one need at a time costs a row of them
// (mw-gq6.358). These cases hold the reads of what stories wait on to one batch
// however many needs there are.

// countedReads is a tracker that counts how the stories it is asked for are
// read: one at a time with ShowStory, or together with ShowBeads.
type countedReads struct {
	*apptest.FakeTracker
	mu      sync.Mutex
	singles int
	batches int
}

func (c *countedReads) ShowStory(ctx context.Context, id string) (application.StoryDetail, error) {
	c.mu.Lock()
	c.singles++
	c.mu.Unlock()
	return c.FakeTracker.ShowStory(ctx, id)
}

func (c *countedReads) ShowBeads(ctx context.Context, ids []string) ([]application.StoryDetail, error) {
	c.mu.Lock()
	c.batches++
	c.mu.Unlock()
	return c.FakeTracker.ShowBeads(ctx, ids)
}

// aStoryWaitingOnMany files, in an epic of their own, three stories and a
// closed fourth, and a story in mw-a that waits on all four.
func aStoryWaitingOnMany(tracker *apptest.FakeTracker) {
	tracker.AddEpic("mw-x", domain.Path{})
	for _, id := range []string{"mw-x.1", "mw-x.2", "mw-x.3", "mw-x.4"} {
		tracker.AddStory("mw-x", domain.Story{ID: id, Title: "Blocker " + id})
	}
	if err := tracker.SetStatus("mw-x.4", apptest.StatusClosed); err != nil {
		panic(err)
	}
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "Waits on many"})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.2", Title: "Waits on one of them"})
	tracker.Needs("mw-a.1", "mw-x.1", "mw-x.2", "mw-x.3", "mw-x.4")
	tracker.Needs("mw-a.2", "mw-x.3")
}

func TestBriefReadsWhatChildrenWaitOnInOneBatch(t *testing.T) {
	tracker := &countedReads{FakeTracker: apptest.NewFakeTracker()}
	aStoryWaitingOnMany(tracker.FakeTracker)

	report, err := application.Brief{Tracker: tracker}.Run(context.Background(), "mw-a")
	if err != nil {
		t.Fatalf("briefing: %v", err)
	}
	waits := map[string][]string{}
	for _, line := range report.Beads[0].Open {
		waits[line.Story.Story.ID] = line.Waits
	}
	if got := waits["mw-a.1"]; len(got) != 3 || got[0] != "Blocker mw-x.1" {
		t.Errorf("expected mw-a.1 to wait on the three unfinished blockers, got %v", got)
	}
	if got := waits["mw-a.2"]; len(got) != 1 || got[0] != "Blocker mw-x.3" {
		t.Errorf("expected mw-a.2 to wait on Blocker mw-x.3, got %v", got)
	}
	if tracker.singles != 0 || tracker.batches != 1 {
		t.Errorf("expected the blockers read in one batch and none singly, got %d batches and %d single reads", tracker.batches, tracker.singles)
	}
}

func TestPosternSnapshotReadsWhatChildrenWaitOnInOneBatch(t *testing.T) {
	tracker := &countedReads{FakeTracker: apptest.NewFakeTracker()}
	aStoryWaitingOnMany(tracker.FakeTracker)
	// An open story that waits is not working; one in progress is, with its waits.
	if err := tracker.SetStatus("mw-a.1", apptest.StatusInProgress); err != nil {
		t.Fatal(err)
	}

	doc, err := application.PosternSnapshot{
		Tracker: tracker,
		Notes:   tracker,
		Now:     func() time.Time { return snapshotNow },
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("building the snapshot: %v", err)
	}
	var waits []string
	for _, w := range epicOf(t, doc, "mw-a").Working {
		if w.ID == "mw-a.1" {
			waits = w.Waits
		}
	}
	if len(waits) != 3 {
		t.Errorf("expected mw-a.1 to wait on the three unfinished blockers, got %v", waits)
	}
	if tracker.singles != 0 || tracker.batches != 1 {
		t.Errorf("expected the blockers read in one batch and none singly, got %d batches and %d single reads", tracker.batches, tracker.singles)
	}
}

func TestDispatchReadsAllWhatACandidateWaitsOnInOneBatch(t *testing.T) {
	dispatch, fake, _, _, _ := aFactory(t)
	tracker := &countedReads{FakeTracker: fake}
	dispatch.Tracker = tracker
	for _, id := range []string{"mw-gq6.1", "mw-gq6.2", "mw-gq6.3"} {
		fake.AddStory("mw-gq6", domain.Story{ID: id, Title: "Blocker " + id})
	}
	if err := fake.SetStatus("mw-gq6.1", apptest.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	fake.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "Waits on three"})
	fake.Needs("mw-gq6.9", "mw-gq6.1", "mw-gq6.2", "mw-gq6.3")

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	var passed bool
	for _, p := range report.Passed {
		if p.StoryID == "mw-gq6.9" {
			passed = true
		}
	}
	if !passed {
		t.Fatalf("expected mw-gq6.9 passed over for what it waits on, got %+v", report.Passed)
	}
	if tracker.singles != 0 {
		t.Errorf("expected no single read of what a candidate waits on, got %d", tracker.singles)
	}
	if tracker.batches != 1 {
		t.Errorf("expected what the candidate waits on read in one batch, got %d batches", tracker.batches)
	}
}

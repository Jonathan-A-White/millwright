package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A story queued for a batch has its lease lapse by design while the batch
// waits. Once the batch lands it, the story stays claimed until the batch's
// close writes it closed; whatever else writes a run state in that window
// (mw sweep, a close-out's look for abandoned stories, a dispatch) must leave
// it alone, or the close is lost (mw-5r3p30.1206, batch 580).

// sweepsOnceLanded is a tracker that, each time the batch records a story as
// landed, lets mw sweep run once over the claimed stories of the host before
// the batch goes on to close it: the order the race had.
type sweepsOnceLanded struct {
	application.WorkTracker
	sweep func()
}

func (s sweepsOnceLanded) SetStoryState(ctx context.Context, id, dimension, value, reason string) error {
	err := s.WorkTracker.SetStoryState(ctx, id, dimension, value, reason)
	if err == nil && dimension == application.RunState && value == application.RunLanded {
		s.sweep()
	}
	return err
}

func TestASweepBetweenTheBatchsLandedRecordAndItsCloseLeavesTheStoryToTheBatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 12, 36, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.Clock = func() time.Time { return now.Add(-6 * time.Hour) }
	path := domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "desktop"}
	tracker.AddEpic("mw-x", path)
	report := &application.BatchReport{Rig: "millwright", Number: 3, Target: "main", Base: "origin/main", Commit: "batch0123456789"}
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		tracker.AddStory("mw-x", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, tracker.ClaimStory(ctx, id))
		queue(t, tracker, id)
		detail, err := tracker.ShowStory(ctx, id)
		mustDo(t, err)
		report.Merged = append(report.Merged, application.MergedStory(id, "merge-"+id, detail, path))
	}
	// The queue has waited six hours: the leases ran out at the claim's start plus the lease's time.
	tracker.Clock = func() time.Time { return now }

	var swept []application.SweepReport
	sweep := func() {
		r, err := application.Sweep{Tracker: tracker, Host: "desktop", Now: func() time.Time { return now }}.Run(ctx)
		if err != nil {
			t.Errorf("sweeping: %v", err)
		}
		swept = append(swept, r)
	}

	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	var out bytes.Buffer
	n := logged(application.Next{
		Tracker: sweepsOnceLanded{WorkTracker: tracker, sweep: sweep}, Vault: newFakeVault(),
		Now:  func() time.Time { return now },
		Seat: "builder", Host: "desktop", Out: &out, Err: &out,
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
	}, lands, &portLog{})
	application.BatchLand{Next: n}.RecordLanded(ctx, "/rigs/millwright", report, &application.NextReport{}, func() {})

	if len(swept) == 0 {
		t.Fatalf("expected the sweep to have run between the landed record and the close, it never did")
	}
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		if state, _ := tracker.StoryState(ctx, id, application.RunState); state != application.RunLanded {
			t.Errorf("expected %s still %s=%s after the sweep, got %q", id, application.RunState, application.RunLanded, state)
		}
		detail, err := tracker.ShowStory(ctx, id)
		mustDo(t, err)
		if detail.Status != apptest.StatusClosed {
			t.Errorf("expected the batch to have closed %s, got %q", id, detail.Status)
		}
		for _, comment := range tracker.Comments(id) {
			if strings.Contains(comment, "mw sweep") {
				t.Errorf("expected no sweep finding on %s, got %q", id, comment)
			}
		}
	}
	for _, r := range swept {
		if len(r.Stuck) != 0 {
			t.Errorf("expected no story marked stuck while the batch closes it, got %+v", r.Stuck)
		}
	}
}

func TestSweepNeverMarksAStoryWhoseLastRunLandedItStuck(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 12, 37, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	claimedStory(t, tracker, "mw-gq6.9", 1)
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunLanded, "landed in batch 3"))
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.9", now.Add(-time.Hour)))

	report, err := application.Sweep{Tracker: tracker, Host: "vps", Now: func() time.Time { return now }}.Run(ctx)
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	if len(report.Stuck) != 0 {
		t.Fatalf("expected no story marked stuck, got %+v", report.Stuck)
	}
	if len(report.Landed) != 1 || report.Landed[0] != "mw-gq6.9" {
		t.Fatalf("expected the sweep to say mw-gq6.9 landed, got %+v", report.Landed)
	}
	if !strings.Contains(report.String(), "landed, closing, not stuck: mw-gq6.9") {
		t.Fatalf("expected the report to name the landed story, got %q", report.String())
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunLanded {
		t.Fatalf("expected mw-gq6.9 still %s=%s, got %q", application.RunState, application.RunLanded, state)
	}
	if comments := tracker.Comments("mw-gq6.9"); len(comments) != 0 {
		t.Fatalf("expected no comment on a landed story, got %q", comments)
	}
}

func TestSweepReadsTheRunStateAgainBeforeItMarksAStoryStuck(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 19, 12, 37, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-gq6", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "vps"})
	claimedStory(t, tracker, "mw-gq6.9", 1)
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.9", now.Add(-time.Hour)))
	// The batch lands the story after the sweep listed it and before it wrote.
	landing := landsWhenAsked{WorkTracker: tracker, id: "mw-gq6.9"}

	report, err := application.Sweep{Tracker: landing, Host: "vps", Now: func() time.Time { return now }}.Run(ctx)
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	if len(report.Stuck) != 0 {
		t.Fatalf("expected no story marked stuck, got %+v", report.Stuck)
	}
	if state, _ := tracker.StoryState(ctx, "mw-gq6.9", application.RunState); state != application.RunLanded {
		t.Fatalf("expected mw-gq6.9 still %s=%s, got %q", application.RunState, application.RunLanded, state)
	}
}

// landsWhenAsked records id as landed the moment the sweep reads its stale
// claims, which is after the sweep listed what is claimed.
type landsWhenAsked struct {
	application.WorkTracker
	id string
}

func (l landsWhenAsked) StaleClaims(ctx context.Context, now time.Time) ([]application.StoryDetail, error) {
	stale, err := l.WorkTracker.StaleClaims(ctx, now)
	if err != nil {
		return nil, err
	}
	return stale, l.WorkTracker.SetStoryState(ctx, l.id, application.RunState, application.RunLanded, "landed in batch 3")
}

func TestNextDoesNotCallALandedStoryAbandonedWhileItsBatchClosesIt(t *testing.T) {
	ctx := context.Background()
	lands := &fakeRetryLanding{}
	next, tracker := aNextWithAStrandedClaim(t, 2*time.Hour, lands, lands, aTranscript(""))
	mustDo(t, tracker.SetStoryState(ctx, "mw-x.2", application.RunState, application.RunLanded, "landed in batch 3"))

	report, err := next.Run(ctx, "mw-x.1")
	if err != nil {
		t.Fatalf("mw next: %v", err)
	}
	if len(report.Abandoned) != 0 || len(report.GaveBack) != 0 {
		t.Fatalf("expected the landed story neither abandoned nor given back, got abandoned %q, gave back %q", report.Abandoned, report.GaveBack)
	}
	if state, _ := tracker.StoryState(ctx, "mw-x.2", application.RunState); state != application.RunLanded {
		t.Fatalf("expected mw-x.2 still %s=%s, got %q", application.RunState, application.RunLanded, state)
	}
	detail, err := tracker.ShowStory(ctx, "mw-x.2")
	mustDo(t, err)
	if detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the landed story's claim kept for its batch's close, got %q", detail.Status)
	}
}

func TestABatchKeepsTheLeaseAndTheClosingOutMarkOfItsQueueWhileItRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, 11, 19, 12, 36, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.Clock = func() time.Time { return now.Add(-6 * time.Hour) }
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet, Effort: domain.EffortHigh, Host: "desktop"})
	var queued []application.BatchStory
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		tracker.AddStory("mw-x", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, tracker.ClaimStory(ctx, id))
		queue(t, tracker, id)
		queued = append(queued, application.BatchStory{ID: id})
	}
	tracker.Clock = func() time.Time { return now }
	land := application.BatchLand{Next: application.Next{
		Tracker: tracker, Host: "desktop", Now: func() time.Time { return now },
		// Between its ticks the heartbeat waits until the batch is done.
		HeartbeatWait: func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() },
	}}
	if stale, _ := tracker.StaleClaims(ctx, now); len(stale) != 2 {
		t.Fatalf("expected both queued stories' leases to have run out to begin with, got %d", len(stale))
	}

	release := land.HoldQueue(ctx, queued)

	if stale, _ := tracker.StaleClaims(ctx, now); len(stale) != 0 {
		t.Errorf("expected no lease to have run out while the batch holds its queue, got %+v", stale)
	}
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		detail, err := tracker.ShowStory(ctx, id)
		mustDo(t, err)
		if !detail.ClosingOut.Live(now) || detail.ClosingOut.By != "desktop" {
			t.Errorf("expected %s to carry the batch's closing-out mark, got %+v", id, detail.ClosingOut)
		}
	}
	release()
	for _, id := range []string{"mw-x.1", "mw-x.2"} {
		detail, err := tracker.ShowStory(ctx, id)
		mustDo(t, err)
		if detail.ClosingOut.Live(now) {
			t.Errorf("expected the mark of %s cleared when the batch is done, got %+v", id, detail.ClosingOut)
		}
	}
}

func TestADispatchLeavesALandedStoryWhoseBatchHoldsItToTheBatch(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{MergedTip: landedTip})
	now := time.Date(2026, 9, 30, 23, 20, 0, 0, time.UTC)
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunLanded, "landed in batch 3"))
	mark := application.ClosingOut{By: "vps", Since: now.Add(-time.Minute), Until: now.Add(4 * time.Minute)}
	mustDo(t, tracker.SetStoryMetadata(ctx, "mw-gq6.9", map[string]string{application.ClosingOutField: mark.String()}))

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(report.LandedAlready) != 0 {
		t.Fatalf("expected the dispatch not to close a story its batch is closing, got %+v", report.LandedAlready)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	mustDo(t, err)
	if detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the claim left for the batch to close, got %q", detail.Status)
	}
}

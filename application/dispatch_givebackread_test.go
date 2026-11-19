package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func noWait(context.Context, time.Duration) error { return nil }

// TestDispatchHeldGiveBackIsReadBack pins mw-gq6.1493: dispatch's give-back of
// a story the Mayor held after the claim wrote its 'gave the claim back'
// comment as soon as HoldClaimHeldBy returned, though bd can say a write is
// done and the story not change (mw-gq6.1465 found it for mw retry). The
// comment is written only once the story reads held and unclaimed.
func TestDispatchHeldGiveBackIsReadBack(t *testing.T) {
	setup := func(t *testing.T) (application.Dispatch, *apptest.FakeTracker) {
		dispatch, tracker, _, _, _ := aFactory(t)
		tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "One"})
		tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
		dispatch.Tracker = &heldTracker{FakeTracker: tracker, afterClaim: true}
		dispatch.Wait = noWait
		return dispatch, tracker
	}
	gaveBack := func(tracker *apptest.FakeTracker) bool {
		for _, comment := range tracker.Comments("mw-gq6.1") {
			if strings.Contains(comment, "gave the claim back") {
				return true
			}
		}
		return false
	}

	t.Run("a write that was lost once is tried again and the story ends held", func(t *testing.T) {
		dispatch, tracker := setup(t)
		tracker.FailReleases(apptest.ReleaseFault{})
		if _, err := dispatch.Run(context.Background()); err != nil {
			t.Fatalf("dispatching: %v", err)
		}
		detail, err := tracker.ShowStory(context.Background(), "mw-gq6.1")
		if err != nil {
			t.Fatal(err)
		}
		if !detail.Held() || detail.Assignee != "" {
			t.Errorf("expected the story held and unclaimed, got %q held by %q", detail.Status, detail.Assignee)
		}
		if !gaveBack(tracker) {
			t.Errorf("expected the comment once the story read held, got %q", tracker.Comments("mw-gq6.1"))
		}
	})

	t.Run("a write that never takes leaves no comment and an error", func(t *testing.T) {
		dispatch, tracker := setup(t)
		tracker.FailReleases(apptest.ReleaseFault{}, apptest.ReleaseFault{}, apptest.ReleaseFault{}, apptest.ReleaseFault{})
		report, err := dispatch.Run(context.Background())
		if gaveBack(tracker) {
			t.Errorf("expected no comment saying the claim was given back, got %q", tracker.Comments("mw-gq6.1"))
		}
		failed := err != nil || len(report.Failed) > 0
		for _, note := range report.Notes {
			if strings.Contains(note, "mw-gq6.1") {
				failed = true
			}
		}
		if !failed {
			t.Errorf("expected an error saying the claim was not given back, got none; report %+v", report)
		}
		detail, serr := tracker.ShowStory(context.Background(), "mw-gq6.1")
		if serr != nil {
			t.Fatal(serr)
		}
		if detail.Assignee == "" {
			t.Errorf("expected the story still claimed, got status %q", detail.Status)
		}
	})
}

// TestHarnessReclaimReadsTheGiveBackBack pins the same for the give-back of a
// story the harness stopped, once the time it named has come (mw-gq6.1493).
func TestHarnessReclaimReadsTheGiveBackBack(t *testing.T) {
	setup := func(t *testing.T) (application.Dispatch, *apptest.FakeTracker) {
		ctx := context.Background()
		dispatch, tracker, _, runner, vaultDir := aFactory(t)
		now := time.Date(2026, 10, 16, 13, 5, 0, 0, time.UTC)
		dispatch.Now = func() time.Time { return now }
		tracker.Clock = func() time.Time { return now }
		dispatch.Memory = tracker
		dispatch.Wait = noWait
		stoppedByTheHarness(t, tracker, runner, vaultDir, "mw-gq6.9", 1, weeklyLimit)
		_ = ctx
		return dispatch, tracker
	}
	gaveBack := func(tracker *apptest.FakeTracker) bool {
		for _, comment := range tracker.Comments("mw-gq6.9") {
			if strings.Contains(comment, "gave the claim on mw-gq6.9 back itself") {
				return true
			}
		}
		return false
	}

	t.Run("a write that was lost once is tried again", func(t *testing.T) {
		dispatch, tracker := setup(t)
		tracker.FailReleases(apptest.ReleaseFault{})
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatalf("dispatching: %v", err)
		}
		if len(report.HarnessRetried) != 1 || !gaveBack(tracker) {
			t.Errorf("expected the story given back and said so, got %+v and %q", report.HarnessRetried, tracker.Comments("mw-gq6.9"))
		}
	})

	t.Run("a write that never takes leaves no comment and no retried story", func(t *testing.T) {
		dispatch, tracker := setup(t)
		tracker.FailReleases(apptest.ReleaseFault{}, apptest.ReleaseFault{}, apptest.ReleaseFault{}, apptest.ReleaseFault{})
		report, _ := dispatch.Run(context.Background())
		if len(report.HarnessRetried) != 0 {
			t.Errorf("expected nothing reported retried, got %+v", report.HarnessRetried)
		}
		if gaveBack(tracker) {
			t.Errorf("expected no comment saying the claim was given back, got %q", tracker.Comments("mw-gq6.9"))
		}
		said := false
		for _, note := range report.Notes {
			if strings.Contains(note, "mw-gq6.9") && strings.Contains(note, "could not be given back") {
				said = true
			}
		}
		if !said {
			t.Errorf("expected a note that the claim could not be given back, got %q", report.Notes)
		}
	})
}

// lostReclaimTracker is a tracker whose ReclaimStory says it gave a claim back
// and, for the first lost calls, changes nothing, as bd can (mw-gq6.372).
type lostReclaimTracker struct {
	*apptest.FakeTracker
	lost int
}

func (l *lostReclaimTracker) ReclaimStory(ctx context.Context, id string) (bool, error) {
	if l.lost > 0 {
		l.lost--
		return true, nil
	}
	return l.FakeTracker.ReclaimStory(ctx, id)
}

// TestDeadPaneReclaimReadsTheGiveBackBack: the dead-pane reclaim's comment says
// the claim was given back only once the story reads open and unclaimed.
func TestDeadPaneReclaimReadsTheGiveBackBack(t *testing.T) {
	setup := func(t *testing.T, lost int) (application.Dispatch, *apptest.FakeTracker) {
		dispatch, tracker, _, runner, _ := aFactory(t)
		now := time.Date(2026, 9, 24, 23, 30, 0, 0, time.UTC)
		dispatch.Now = func() time.Time { return now }
		tracker.Clock = func() time.Time { return now }
		dispatch.Wait = noWait
		claimedStory(t, tracker, "mw-gq6.9", 1)
		if err := tracker.SetLeaseExpires("mw-gq6.9", now.Add(-33*time.Minute)); err != nil {
			t.Fatal(err)
		}
		runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 1)
		dispatch.Tracker = &lostReclaimTracker{FakeTracker: tracker, lost: lost}
		return dispatch, tracker
	}
	gaveBack := func(tracker *apptest.FakeTracker) bool {
		for _, comment := range tracker.Comments("mw-gq6.9") {
			if strings.Contains(comment, "dead pane") {
				return true
			}
		}
		return false
	}

	t.Run("a write that was lost once is tried again", func(t *testing.T) {
		dispatch, tracker := setup(t, 1)
		report, err := dispatch.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Reclaimed) != 1 || !gaveBack(tracker) {
			t.Errorf("expected the story reclaimed and said so, got %+v and %q", report.Reclaimed, tracker.Comments("mw-gq6.9"))
		}
	})

	t.Run("a write that never takes leaves no comment and no reclaimed story", func(t *testing.T) {
		dispatch, tracker := setup(t, 10)
		report, _ := dispatch.Run(context.Background())
		if len(report.Reclaimed) != 0 || gaveBack(tracker) {
			t.Errorf("expected nothing reclaimed and no comment, got %+v and %q", report.Reclaimed, tracker.Comments("mw-gq6.9"))
		}
		said := false
		for _, note := range report.Notes {
			if strings.Contains(note, "mw-gq6.9") && strings.Contains(note, "could not be given back") {
				said = true
			}
		}
		if !said {
			t.Errorf("expected a note that the claim could not be given back, got %q", report.Notes)
		}
	})
}

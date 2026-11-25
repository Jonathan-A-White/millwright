package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// TestGivenBackAsJudgesTheGiveBackByTheStory pins mw-gq6.1493 for the stop
// give-backs of mw next: a write that returned nil and left the story claimed
// is an error, and a failed write that left it given back did land.
func TestGivenBackAsJudgesTheGiveBackByTheStory(t *testing.T) {
	ctx := context.Background()
	claimed := func(t *testing.T) *apptest.FakeTracker {
		tracker := apptest.NewFakeTracker()
		tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
		if err := tracker.ClaimStory(ctx, "mw-gq6.1"); err != nil {
			t.Fatal(err)
		}
		return tracker
	}
	t.Run("a write lost with no error is an error", func(t *testing.T) {
		tracker := claimed(t)
		tracker.FailReleases(apptest.ReleaseFault{})
		err := application.GivenBackAs(ctx, tracker, "mw-gq6.1", application.StatusOpen, tracker.ReleaseClaim(ctx, "mw-gq6.1"))
		if err == nil || !strings.Contains(err.Error(), "still reads") {
			t.Fatalf("expected an error saying the story still reads claimed, got %v", err)
		}
	})
	t.Run("a write that took is nil", func(t *testing.T) {
		tracker := claimed(t)
		if err := application.GivenBackAs(ctx, tracker, "mw-gq6.1", application.StatusOpen, tracker.ReleaseClaim(ctx, "mw-gq6.1")); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
	t.Run("a held give-back is judged held", func(t *testing.T) {
		tracker := claimed(t)
		if err := application.GivenBackAs(ctx, tracker, "mw-gq6.1", application.StatusHeld, tracker.HoldClaimHeldBy(ctx, "mw-gq6.1", "", time.Time{})); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		if err := application.GivenBackAs(ctx, tracker, "mw-gq6.1", application.StatusOpen, nil); err == nil {
			t.Fatal("expected a held story not to read as given back open")
		}
	})
	t.Run("a failed write on a story already given back did land", func(t *testing.T) {
		tracker := claimed(t)
		if err := tracker.ReleaseClaim(ctx, "mw-gq6.1"); err != nil {
			t.Fatal(err)
		}
		if err := application.GivenBackAs(ctx, tracker, "mw-gq6.1", application.StatusOpen, errors.New("connection dropped")); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}

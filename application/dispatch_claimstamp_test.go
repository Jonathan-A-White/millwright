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

// claimReadingRunner reads the story's claim age at the moment its session is
// started, which is the whole of the window mw next can meet a claim in whose
// session is not there yet (mw-thms99).
type claimReadingRunner struct {
	*apptest.FakeRunner
	tracker *apptest.FakeTracker
	id      string
	seen    application.StoryDetail
}

func (r *claimReadingRunner) Start(ctx context.Context, spec application.SessionSpec) error {
	r.seen, _ = r.tracker.ShowStory(ctx, r.id)
	return r.FakeRunner.Start(ctx, spec)
}

// mw-thms99: a dispatch stamped claimed_at only once the session was up, so a
// story claimed again read, for the minute the session took to start, as
// claimed at its earlier attempt's time and mw next gave the claim back as one
// whose session never started. The claim is stamped when it is made.
func TestDispatchStampsTheClaimBeforeItStartsTheSession(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	now := time.Date(2026, 10, 24, 3, 15, 24, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	// An earlier attempt's claim, long ago.
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.1", map[string]string{
		application.ClaimedAtField: now.Add(-3 * time.Hour).Format(time.RFC3339),
		application.AttemptsField:  "1",
	}); err != nil {
		t.Fatalf("setting the earlier claim: %v", err)
	}
	reading := &claimReadingRunner{FakeRunner: runner, tracker: tracker, id: "mw-gq6.1"}
	dispatch.Runner = reading

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := reading.seen.ClaimStarted(); !got.Equal(now) {
		t.Fatalf("expected the claim stamped %s before the session started, got %s", now, got)
	}
	if age := now.Sub(reading.seen.ClaimStarted()); age >= application.NeverStartedGrace {
		t.Fatalf("expected a claim younger than the grace of %s, got %s", application.NeverStartedGrace, age)
	}
}

// mw-thms99: a dispatch that claims a story whose session is already running
// here keeps the claim with it, starts nothing, and says so once.
func TestDispatchKeepsTheClaimOfAStoryWhoseSessionIsAlreadyRunning(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	session := startOldSession(t, runner, "mw-gq6.1")

	for tick := 1; tick <= 2; tick++ {
		report, err := dispatch.Run(ctx)
		if err != nil {
			t.Fatalf("tick %d: expected a running namesake not to fail the dispatch, got %v", tick, err)
		}
		if len(report.Started) != 0 || len(report.Failed) != 0 {
			t.Fatalf("tick %d: expected nothing started and nothing failed, got %+v", tick, report)
		}
	}
	if names := runner.Started(); len(names) != 1 {
		t.Errorf("expected only the earlier session, got %q started", names)
	}
	if status, _ := runner.Status(ctx, session); !status.Running() {
		t.Errorf("expected the running session left alone, got %+v", status)
	}
	if closed := runner.Closed(); len(closed) != 0 {
		t.Errorf("expected nothing closed, got %q", closed)
	}
	if added, removed := worktrees.was(); len(added)+len(removed) != 0 {
		t.Errorf("expected no worktree cut or removed, got %q added and %q removed", added, removed)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Errorf("expected the claim kept, got status %q assignee %q", detail.Status, detail.Assignee)
	}
	var said int
	for _, c := range tracker.Comments("mw-gq6.1") {
		if strings.Contains(c, "already running here") && strings.Contains(c, "the claim stays with it") {
			said++
		}
	}
	if said != 1 {
		t.Errorf("expected it said once on the story, got %d times: %q", said, tracker.Comments("mw-gq6.1"))
	}
}

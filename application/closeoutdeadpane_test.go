package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.797: a close-out that failed left its tmux session as a dead pane for
// days (mw-5r3p30.300, 7 days), and the story sat in progress with nothing but
// that pane to say why. The close-out's words are kept in the story's run
// (next-<attempt>.log), so a refused story's session is closed and the story is
// left run=blocked, which mw status names.

// A refused story still in progress whose close-out log is kept in its run: the
// session is closed, the claim kept, run=blocked recorded, and mw status names
// it as refused.
func TestARefusedCloseOutClosesItsSessionWhenItsLogIsKept(t *testing.T) {
	ctx := context.Background()
	next, tracker, runner := aNextWithNothingToLand(t)
	mustDo(t, tracker.ClaimStory(ctx, "mw-x.1"))
	if _, err := next.Vault.PutRunFile(ctx, "mw-x.1", application.NextLogNameForAttempt(1), "closing out\n"); err != nil {
		t.Fatal(err)
	}

	report, err := next.Run(ctx, "mw-x.1")
	if err == nil || report.Reason != application.ReasonNoCommits {
		t.Fatalf("expected a refusal for no-commits, got %v and reason %q", err, report.Reason)
	}
	if !closedSessions(runner) {
		t.Errorf("expected the session of a refused close-out with a kept log to be closed, closed: %q", runner.Closed())
	}
	if run, err := tracker.StoryState(ctx, "mw-x.1", application.RunState); err != nil || run != application.RunBlocked {
		t.Errorf("expected the story left %s=%s for mw status to name, got %q (%v)", application.RunState, application.RunBlocked, run, err)
	}
	detail, err := tracker.ShowStory(ctx, "mw-x.1")
	if err != nil || detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Errorf("expected the claim kept, got status %q assignee %q (%v)", detail.Status, detail.Assignee, err)
	}
}

// A dead pane found by mw dispatch for a story its close-out refused, whose log
// is kept, is closed; the story stays claimed and held as refused.
func TestDispatchClosesTheDeadPaneOfARefusedStoryWhoseLogIsKept(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	now := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	claimedStory(t, tracker, "mw-gq6.9", 1)
	mustDo(t, tracker.SetStoryState(ctx, "mw-gq6.9", application.RunState, application.RunBlocked, "(no-findings) the Tester story names no landed story"))
	runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 1)
	if _, err := dispatch.Boot.Vault.PutRunFile(ctx, "mw-gq6.9", application.NextLogNameForAttempt(1), "Error: closing out mw-gq6.9\n"); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(report.HeldRefused) != 1 || len(report.Reclaimed) != 0 {
		t.Fatalf("expected the story held as refused, got %+v", report)
	}
	if got := runner.Closed(); len(got) != 1 || got[0] != application.SessionName("mw-gq6.9") {
		t.Errorf("expected the dead pane closed, closed: %q", got)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil || detail.Status != apptest.StatusInProgress || detail.Assignee == "" {
		t.Errorf("expected the claim kept, got status %q assignee %q (%v)", detail.Status, detail.Assignee, err)
	}
	if !strings.Contains(report.String(), "window closed") {
		t.Errorf("expected the report to say the window was closed, got:\n%s", report.String())
	}
}

package application_test

import (
	"context"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// plainSuccessResult is the result of a session that finished its turn.
const plainSuccessResult = `{"type":"result","subtype":"success","is_error":false,"num_turns":9,` +
	`"duration_ms":600000,"session_id":"s-plain","total_cost_usd":1.5,` +
	`"usage":{"input_tokens":100,"output_tokens":2000,"cache_read_input_tokens":50000,"cache_creation_input_tokens":3000}}`

// aNextWithNothingToLand is the mw next that closes out mw-x.1, a story whose
// branch holds no commits, in the session the story was worked in, which has
// ended and is still on the terminal (mw-gq6.687).
func aNextWithNothingToLand(t *testing.T) (application.Next, *apptest.FakeTracker, *apptest.FakeRunner) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story that committed nothing"})
	runner := apptest.NewFakeRunner()
	name := application.SessionName("mw-x.1")
	if err := runner.Start(context.Background(), application.SessionSpec{Name: name, Command: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	runner.Exit(name, 0)
	vault := newFakeVault()
	if _, err := vault.PutRunFile(context.Background(), "mw-x.1", "result.json", plainSuccessResult); err != nil {
		t.Fatal(err)
	}
	lands := &fakeRetryLanding{}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	return application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}, tracker, runner
}

func closedSessions(runner *apptest.FakeRunner) bool {
	return slices.Contains(runner.Closed(), application.SessionName("mw-x.1"))
}

// mw-gq6.687: a close-out that finds nothing to land and leaves the story open
// (the claim was given back, or never held) is a story finished with its
// session: the session is closed, not left with a dead pane.
func TestNothingToLandClosesTheSessionOfAStoryNoLongerInProgress(t *testing.T) {
	next, _, runner := aNextWithNothingToLand(t)

	report, err := next.Run(context.Background(), "mw-x.1")
	if err == nil || report.Reason != application.ReasonNoCommits {
		t.Fatalf("expected a refusal for no-commits, got %v and reason %q", err, report.Reason)
	}
	if !closedSessions(runner) {
		t.Errorf("expected the session of a story that is not in progress to be closed, closed: %q", runner.Closed())
	}
}

// A story closed by something else is refused as closed already, and its
// session is finished with too.
func TestNothingToLandClosesTheSessionOfAStoryClosedAlready(t *testing.T) {
	next, tracker, runner := aNextWithNothingToLand(t)
	mustDo(t, tracker.SetStatus("mw-x.1", apptest.StatusClosed))

	if _, err := next.Run(context.Background(), "mw-x.1"); err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if !closedSessions(runner) {
		t.Errorf("expected the session of a closed story to be closed, closed: %q", runner.Closed())
	}
}

// The evidence rule (mw-gq6.61, mw-gq6.182): a refused story still in progress
// keeps its claim, and its session keeps its dead pane on screen for the Mayor.
func TestNothingToLandKeepsTheDeadPaneOfARefusedStoryStillInProgress(t *testing.T) {
	next, tracker, runner := aNextWithNothingToLand(t)
	mustDo(t, tracker.ClaimStory(context.Background(), "mw-x.1"))

	report, err := next.Run(context.Background(), "mw-x.1")
	if err == nil || report.Reason != application.ReasonNoCommits {
		t.Fatalf("expected a refusal for no-commits, got %v and reason %q", err, report.Reason)
	}
	if closedSessions(runner) {
		t.Errorf("expected the dead pane of a refused in-progress story to stay, closed: %q", runner.Closed())
	}
}

// A story whose session is still at work is refused untouched, and its session
// is the one thing that must not be closed.
func TestNothingToLandLeavesARunningSessionAlone(t *testing.T) {
	next, _, runner := aNextWithNothingToLand(t)
	beats := apptest.NewFakeHeartbeats()
	beats.Start("mw-x.1", 4242)
	next.Heartbeats = beats

	if _, err := next.Run(context.Background(), "mw-x.1"); err == nil {
		t.Fatal("expected a refusal, got none")
	}
	if closedSessions(runner) {
		t.Errorf("expected a session still at work to be left, closed: %q", runner.Closed())
	}
}

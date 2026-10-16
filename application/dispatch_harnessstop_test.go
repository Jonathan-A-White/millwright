package application_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.434: on 2026-10-15 fourteen stories each burned an attempt on turn 1
// when the harness's weekly limit stopped their sessions, and each needed a hand
// mw retry. A dispatch now claims nothing new on its host until the reset time
// the harness named, and then gives those stories back itself, the stopped
// attempt not counted.

const weeklyLimit = "You've hit your weekly limit · resets Oct 16, 9am (America/New_York)"

// stoppedByTheHarness adds a story claimed here whose close-out refused it
// (run=blocked) after its attempt-th session's result said said, as mw next
// leaves a session the harness stopped: the claim kept, the pane dead.
func stoppedByTheHarness(t *testing.T, tracker *apptest.FakeTracker, runner *apptest.FakeRunner, vaultDir, id string, attempt int, said string) {
	t.Helper()
	claimedStory(t, tracker, id, attempt)
	if err := tracker.SetStoryState(context.Background(), id, application.RunState, application.RunBlocked,
		"(session-failed) the session did not finish: "+said); err != nil {
		t.Fatalf("recording the refusal: %v", err)
	}
	result := fmt.Sprintf(`{"type":"result","subtype":"success","is_error":true,"num_turns":1,"duration_ms":60000,"result":%q}`, said)
	dir := filepath.Join(vaultDir, application.RunsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileNameForAttempt(attempt)), []byte(result), 0o644); err != nil {
		t.Fatalf("writing the result: %v", err)
	}
	runner.Exit(startOldSession(t, runner, id), 0)
}

func TestDispatchClaimsNothingNewBeforeTheResetTimeAUsageLimitNames(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	dispatch.Cap = 2
	stoppedByTheHarness(t, tracker, runner, vaultDir, "mw-gq6.9", 1, weeklyLimit)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story ready to be worked"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 0 || len(report.HarnessRetried) != 0 {
		t.Fatalf("expected nothing claimed before the reset, got %+v", report)
	}
	reset := time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)
	if report.HarnessHold == nil || !report.HarnessHold.Until.Equal(reset) || report.HarnessHold.Reason != application.ReasonUsageLimit {
		t.Fatalf("expected the host held for the usage limit until %s, got %+v", reset, report.HarnessHold)
	}
	var why string
	for _, passed := range report.Passed {
		if passed.StoryID == "mw-gq6.1" {
			why = passed.Why
		}
	}
	if !strings.Contains(why, string(application.ReasonUsageLimit)) || !strings.Contains(why, "2026-10-16T13:00:00Z") {
		t.Fatalf("expected the ready story passed over until the reset, naming it, got %q", why)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Status != apptest.StatusInProgress || detail.Assignee == "" || detail.Attempts != 1 {
		t.Fatalf("expected the stopped story still held as it was, got status %q assignee %q attempts %d",
			detail.Status, detail.Assignee, detail.Attempts)
	}
	if note, _ := tracker.Note(ctx, application.HarnessHoldKey("vps")); !strings.HasPrefix(note, "2026-10-16T13:00:00Z usage-limit") {
		t.Fatalf("expected the hold kept in the host's note, got %q", note)
	}
}

func TestDispatchRetriesAUsageLimitedStoryItselfAfterTheResetWithoutCountingTheAttempt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 10, 16, 13, 5, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	stoppedByTheHarness(t, tracker, runner, vaultDir, "mw-gq6.9", 1, weeklyLimit)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if report.HarnessHold != nil {
		t.Fatalf("expected no hold once the reset has passed, got %+v", report.HarnessHold)
	}
	if len(report.HarnessRetried) != 1 || report.HarnessRetried[0] != "mw-gq6.9" {
		t.Fatalf("expected the stopped story given back by the dispatch itself, got %+v", report.HarnessRetried)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" || report.Started[0].Attempt != 1 {
		t.Fatalf("expected the story started again as attempt 1, the stopped one not counted, got %+v", report.Started)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Attempts != 1 {
		t.Fatalf("expected the attempt count unchanged by the stopped attempt (1), got %d", detail.Attempts)
	}
	said := false
	for _, comment := range tracker.Comments("mw-gq6.9") {
		if strings.Contains(comment, "not counted") && strings.Contains(comment, string(application.ReasonUsageLimit)) {
			said = true
		}
	}
	if !said {
		t.Fatalf("expected a comment saying the usage-limit attempt was not counted, got %q", tracker.Comments("mw-gq6.9"))
	}
}

func TestDispatchKeepsAStoryTheWorkItselfFailedHeldAsBefore(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 10, 16, 13, 5, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	stoppedByTheHarness(t, tracker, runner, vaultDir, "mw-gq6.9", 1, "the session ran out of fuel")

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if report.HarnessHold != nil || len(report.HarnessRetried) != 0 || len(report.Started) != 0 {
		t.Fatalf("expected the refused story left held and nothing else done, got %+v", report)
	}
	if len(report.HeldRefused) != 1 {
		t.Fatalf("expected the refused story reported held, got %+v", report.HeldRefused)
	}
}

// A hand mw retry gives a usage-limited story back before its reset: the hold it
// leaves in the host's note keeps the dispatch from claiming it, or anything
// else, until then, and is cleared once the time has passed.
func TestDispatchHonoursTheHoldAHandRetryLeftAndClearsItOnceItHasPassed(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story ready to be worked"})
	if err := tracker.SetNote(ctx, application.HarnessHoldKey("vps"), "2026-10-16T13:00:00Z usage-limit "+weeklyLimit); err != nil {
		t.Fatalf("leaving the hold: %v", err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 0 || report.HarnessHold == nil {
		t.Fatalf("expected nothing claimed before the reset, got %+v", report)
	}

	now = time.Date(2026, 10, 16, 13, 0, 1, 0, time.UTC)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.1" || report.HarnessHold != nil {
		t.Fatalf("expected the story claimed once the reset has passed, got %+v", report)
	}
	if note, _ := tracker.Note(ctx, application.HarnessHoldKey("vps")); note != "" {
		t.Fatalf("expected the passed hold cleared, got %q", note)
	}
}

// The sweep: an overloaded API waits a quarter of an hour from when the session
// ended (its claim plus the time it ran), then is retried the same way.
func TestDispatchHoldsAnOverloadedStopAQuarterHourThenRetriesIt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	claimed := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	now := claimed.Add(10 * time.Minute)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	stoppedByTheHarness(t, tracker, runner, vaultDir, "mw-gq6.9", 2,
		`API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	if err := tracker.SetStoryMetadata(ctx, "mw-gq6.9", map[string]string{application.ClaimedAtField: claimed.Format(time.RFC3339)}); err != nil {
		t.Fatalf("recording the claim time: %v", err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	until := claimed.Add(time.Minute + application.HarnessStopWait)
	if report.HarnessHold == nil || !report.HarnessHold.Until.Equal(until) || report.HarnessHold.Reason != application.ReasonOverloaded {
		t.Fatalf("expected the host held for the overloaded API until %s, got %+v", until, report.HarnessHold)
	}

	now = until.Add(time.Second)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].Attempt != 2 {
		t.Fatalf("expected the story started again as attempt 2, got %+v", report.Started)
	}
}

package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.471: a usage limit that leaves no result JSON was read by mw next as
// a missing result, its attempt counted; and mw status read the limit with a
// regex of its own, so the two could disagree on the same words. One reader of
// the limit notice now serves next, status, dispatch and retry.

func TestTheOneReaderOfTheUsageLimitTakesEitherApostrophe(t *testing.T) {
	ended := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	for _, said := range []string{
		"You’ve hit your weekly limit · resets Oct 16, 9am (UTC)",
		"You have hit your session limit · resets 3pm (UTC)",
	} {
		result := application.SessionResult{IsError: true, Said: said}
		stop, ok := application.HarnessStopOf(result, ended)
		if !ok || stop.Reason != application.ReasonUsageLimit {
			t.Fatalf("expected %q read as %s, got %+v (%v)", said, application.ReasonUsageLimit, stop, ok)
		}
		if result.UsageLimit() != said {
			t.Fatalf("expected status to read the same words, got %q", result.UsageLimit())
		}
	}
}

func TestWordsThatAreNotAResultAreReadOnlyForTheUsageLimit(t *testing.T) {
	ended := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	stop, ok := application.HarnessStopOfWords("Error: the session ended\nClaude AI usage limit reached|1792155600\n", ended)
	if !ok || stop.Reason != application.ReasonUsageLimit || !stop.Until.Equal(time.Unix(1792155600, 0)) {
		t.Fatalf("expected the usage limit and its reset read from plain words, got %+v (%v)", stop, ok)
	}
	if stop, ok := application.HarnessStopOfWords(`API Error: 529 {"type":"error","error":{"type":"overloaded_error"}}`, ended); ok {
		t.Fatalf("expected an API error in words that are not a result left alone, got %+v", stop)
	}
}

// A dispatch reads a limit whose result file holds the harness's plain words,
// not JSON, as it reads one in a result: held until the reset, then given back
// with the attempt not counted.
func TestDispatchHoldsAUsageLimitThatLeftNoResultJSONThenRetriesIt(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	stoppedByTheHarness(t, tracker, runner, vaultDir, "mw-gq6.9", 1, weeklyLimit)
	result := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9", application.ResultFileName)
	if err := os.WriteFile(result, []byte("Claude AI usage limit reached|1792155600\n"), 0o644); err != nil {
		t.Fatalf("writing the plain words: %v", err)
	}

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	reset := time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)
	if report.HarnessHold == nil || !report.HarnessHold.Until.Equal(reset) {
		t.Fatalf("expected the host held until %s, got %+v", reset, report.HarnessHold)
	}
	if note, _ := tracker.Note(ctx, application.HarnessHoldKey("vps")); !strings.HasPrefix(note, "2026-10-16T13:00:00Z usage-limit") {
		t.Fatalf("expected the hold kept in the host's note, got %q", note)
	}

	now = reset.Add(time.Minute)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.HarnessRetried) != 1 || len(report.Started) != 1 || report.Started[0].Attempt != 1 {
		t.Fatalf("expected the story given back and started again as attempt 1, got %+v", report)
	}
}

// The whole way through: a session whose result is empty, its pane showing the
// limit, is closed out by mw next as a harness stop; the dispatch after it holds
// the host until the reset and then gives the story back, the attempt not
// counted.
func TestAnEmptyResultThePaneSaysWasLimitedIsHeldAndRetriedFree(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	claimedStory(t, tracker, "mw-gq6.9", 1)
	name := startOldSession(t, runner, "mw-gq6.9")
	runner.Write(name, "You’ve hit your weekly limit · resets Oct 16, 9am (America/New_York)\n")
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileName), nil, 0o644); err != nil {
		t.Fatalf("writing the empty result: %v", err)
	}

	lands := &fakeRetryLanding{}
	next := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: "vps",
		Rigs: dispatch.Rigs,
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}
	closed, _ := next.Run(ctx, "mw-gq6.9")
	if closed.Reason != application.ReasonUsageLimit {
		t.Fatalf("expected mw next to stop for %s, got %q: %s", application.ReasonUsageLimit, closed.Reason, closed.Why)
	}
	runner.Exit(name, 0)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	reset := time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)
	if report.HarnessHold == nil || !report.HarnessHold.Until.Equal(reset) || len(report.Started) != 0 {
		t.Fatalf("expected the host held until %s and nothing started, got %+v", reset, report)
	}
	if note, _ := tracker.Note(ctx, application.HarnessHoldKey("vps")); !strings.HasPrefix(note, "2026-10-16T13:00:00Z usage-limit") {
		t.Fatalf("expected the hold kept in the host's note, got %q", note)
	}

	now = reset.Add(time.Minute)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("expected the dispatch to run cleanly, got %v", err)
	}
	if len(report.HarnessRetried) != 1 || len(report.Started) != 1 || report.Started[0].Attempt != 1 {
		t.Fatalf("expected the story given back and started again as attempt 1, got %+v", report)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.9")
	if err != nil {
		t.Fatalf("showing the story: %v", err)
	}
	if detail.Attempts != 1 {
		t.Fatalf("expected the stopped attempt not counted (1), got %d", detail.Attempts)
	}
	if detail.Status != apptest.StatusInProgress {
		t.Fatalf("expected the story claimed again, got %q", detail.Status)
	}
}

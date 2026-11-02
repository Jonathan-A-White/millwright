package application_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.887: tmux runs under a 5 s deadline (mw-gq6.884), so a look at a
// session's pane that errs is an ordinary event on a loaded host. The close-out
// said nothing of it: an empty result's record left the pane's tail out as if no
// session were left, and a stop shown only in the pane was read as no stop.

// slowPaneRunner is a runner whose Output alone fails the way a loaded tmux does.
type slowPaneRunner struct {
	*apptest.FakeRunner
}

func (s *slowPaneRunner) Output(_ context.Context, name string, _ int) (string, error) {
	return "", errors.New("tmux capture-pane -t =" + name + ": did not finish in 5s and was stopped: context deadline exceeded")
}

// anEmptyResultCloseOut sets up a story whose session wrote an empty result and
// closes it out with the runner given (nil for none); it returns the report and
// the comments left on the story.
func anEmptyResultCloseOut(t *testing.T, paneText string, wrap func(*apptest.FakeRunner) application.Runner) (application.NextReport, string) {
	t.Helper()
	return aResultCloseOut(t, "", paneText, wrap)
}

// aResultCloseOut is the same with the words the session left in its result.
func aResultCloseOut(t *testing.T, printed, paneText string, wrap func(*apptest.FakeRunner) application.Runner) (application.NextReport, string) {
	t.Helper()
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	claimedStory(t, tracker, "mw-gq6.9", 1)
	name := startOldSession(t, runner, "mw-gq6.9")
	if paneText != "" {
		runner.Write(name, paneText)
	}
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileName), []byte(printed), 0o644); err != nil {
		t.Fatalf("writing the empty result: %v", err)
	}
	var runs application.Runner
	if wrap != nil {
		runs = wrap(runner)
	}
	lands := &fakeRetryLanding{}
	next := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runs,
		Seat: "builder", Host: "vps",
		Rigs: dispatch.Rigs,
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}
	report, _ := next.Run(ctx, "mw-gq6.9")
	return report, strings.Join(tracker.Comments("mw-gq6.9"), "\n---\n")
}

func TestAnEmptyResultSaysItsPaneCouldNotBeReadWhenTmuxFailsToAnswer(t *testing.T) {
	report, comments := anEmptyResultCloseOut(t, "working\n", func(r *apptest.FakeRunner) application.Runner {
		return &slowPaneRunner{FakeRunner: r}
	})
	if report.Reason != application.ReasonNoResult {
		t.Fatalf("expected %s, got %q: %s", application.ReasonNoResult, report.Reason, report.Why)
	}
	for _, want := range []string{"pane could not be read", "did not finish in 5s"} {
		if !strings.Contains(comments, want) {
			t.Fatalf("expected the record to say %q, got:\n%s", want, comments)
		}
	}
}

func TestAHarnessStopShownOnlyInThePaneIsNotSilentlyDroppedWhenTmuxFailsToAnswer(t *testing.T) {
	report, comments := anEmptyResultCloseOut(t, "You’ve hit your weekly limit · resets Oct 16, 9am (America/New_York)\n",
		func(r *apptest.FakeRunner) application.Runner { return &slowPaneRunner{FakeRunner: r} })
	if report.Reason == application.ReasonUsageLimit {
		t.Fatalf("a stop nobody could read cannot have been decided: %+v", report)
	}
	noted := strings.Contains(strings.Join(report.Notes, "\n"), "pane could not be read") || strings.Contains(comments, "pane could not be read")
	if !noted {
		t.Fatalf("expected the close-out to say the pane could not be read, so a stop shown only there is not mistaken for none; notes %v, comments:\n%s", report.Notes, comments)
	}
}

func TestAnEmptyResultRecordIsAsBeforeForASessionWithNothingToShowAndForNoRunner(t *testing.T) {
	// A session that printed nothing: no tail section, and no word of a failed look.
	_, comments := anEmptyResultCloseOut(t, "", func(r *apptest.FakeRunner) application.Runner { return r })
	if strings.Contains(comments, "could not be read") || strings.Contains(comments, "last lines of the session") {
		t.Fatalf("expected a session with nothing to show to leave the record as it was, got:\n%s", comments)
	}
	if !strings.Contains(comments, "is empty") && !strings.Contains(comments, "0 byte(s)") {
		t.Fatalf("expected the empty result's record, got:\n%s", comments)
	}
	// No runner at all: the same.
	_, comments = anEmptyResultCloseOut(t, "", nil)
	if strings.Contains(comments, "could not be read") || strings.Contains(comments, "last lines of the session") {
		t.Fatalf("expected no runner to leave the record as it was, got:\n%s", comments)
	}
	// A session with lines: the tail is shown.
	_, comments = anEmptyResultCloseOut(t, "doing things\n", func(r *apptest.FakeRunner) application.Runner { return r })
	if !strings.Contains(comments, "The last lines of the session's pane") || !strings.Contains(comments, "doing things") {
		t.Fatalf("expected the tail shown, got:\n%s", comments)
	}
}

func TestAResultOfPlainWordsNotesThatThePaneCouldNotBeReadForAStop(t *testing.T) {
	slow := func(r *apptest.FakeRunner) application.Runner { return &slowPaneRunner{FakeRunner: r} }
	report, _ := aResultCloseOut(t, "the session ended\n", "", slow)
	if !strings.Contains(strings.Join(report.Notes, "\n"), "pane could not be read") {
		t.Fatalf("expected a note that the pane could not be read, got %v", report.Notes)
	}
	// A stop in the words printed is still found without the pane.
	report, _ = aResultCloseOut(t, "Claude AI usage limit reached|1792155600\n", "", slow)
	if report.Reason != application.ReasonUsageLimit {
		t.Fatalf("expected the stop in the printed words found, got %q: %s", report.Reason, report.Why)
	}
}

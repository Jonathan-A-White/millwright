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

// mw-gq6.1153: a Builder session that dies at its start with the shell's
// "Segmentation fault" left an empty result and a pane ending in those words,
// and nothing read them, so the box that did it was never named. The close-out
// records it as an integrity fault of the box, the record a corrupt git object
// leaves (mw-gq6.1125), and a second within the hour names the box.

const integrityKey = "millwright.integrity"

// aSegfaultCloseOut closes out a story whose session left an empty result and a
// pane showing paneText, on box cloud2 keeping its integrity marks in marks.
func aSegfaultCloseOut(t *testing.T, paneText string, marks fakeBuilt, now time.Time) application.NextReport {
	t.Helper()
	ctx := context.Background()
	dispatch, tracker, _, runner, vaultDir := aFactory(t)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Memory = tracker
	claimedStory(t, tracker, "mw-gq6.9", 1)
	name := startOldSession(t, runner, "mw-gq6.9")
	runner.Write(name, paneText)
	dir := filepath.Join(vaultDir, application.RunsDir, "mw-gq6.9")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, application.ResultFileName), nil, 0o644); err != nil {
		t.Fatalf("writing the empty result: %v", err)
	}
	lands := &fakeRetryLanding{}
	report, _ := application.Next{
		Tracker: tracker, Vault: dispatch.Boot.Vault, Worktrees: lands, Landing: lands,
		Checks: checkStub{}, Slot: slotStub{}, Runner: runner,
		Seat: "builder", Host: "cloud2", Box: "cloud2", Integrity: marks,
		Rigs: dispatch.Rigs,
		Now:  func() time.Time { return now },
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-gq6.9")
	return report
}

func TestASessionThatDiesAtItsStartWithASegmentationFaultIsRecordedAsAnIntegrityFaultOfItsBox(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	marks := fakeBuilt{}
	report := aSegfaultCloseOut(t, "starting claude\nSegmentation fault (core dumped)\n", marks, now)

	if got := marks[integrityKey]; got != now.Format(time.RFC3339) {
		t.Fatalf("expected the fault recorded at %s, got %q", now.Format(time.RFC3339), got)
	}
	// The story's own stop is as it was: no result, nothing merged.
	if report.Reason != application.ReasonNoResult || report.Landed {
		t.Fatalf("expected the %s stop unchanged, got %q landed=%v: %s", application.ReasonNoResult, report.Reason, report.Landed, report.Why)
	}
	if strings.Contains(strings.Join(report.Notes, "\n"), "mw cloud down") {
		t.Fatalf("expected a first fault not to name the box, got %v", report.Notes)
	}
}

func TestASessionWhosePaneDoesNotEndInASegmentationFaultLeavesNoIntegrityRecord(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	for name, pane := range map[string]string{
		"plain words":      "working\n",
		"an old fault":     "Segmentation fault (core dumped)\nrestarted\nthinking\nwriting\nreading\n",
		"an unrelated end": "● the test for a Segmentation fault passed\nall done\nbye\nbye\nbye\n",
	} {
		marks := fakeBuilt{}
		report := aSegfaultCloseOut(t, pane, marks, now)
		if got := marks[integrityKey]; got != "" {
			t.Errorf("%s: expected no record, got %q", name, got)
		}
		if report.Reason != application.ReasonNoResult {
			t.Errorf("%s: expected %s, got %q", name, application.ReasonNoResult, report.Reason)
		}
	}
}

func TestASecondSegmentationFaultWithinTheHourNamesTheBoxWithMwCloudDown(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	pane := "Segmentation fault (core dumped)\n"

	soon := fakeBuilt{integrityKey: now.Add(-30 * time.Minute).Format(time.RFC3339)}
	report := aSegfaultCloseOut(t, pane, soon, now)
	if !strings.Contains(strings.Join(report.Notes, "\n"), "mw cloud down cloud2") {
		t.Fatalf("expected the second fault within the hour to name the box, got %v", report.Notes)
	}
	if soon[integrityKey] != now.Format(time.RFC3339) {
		t.Fatalf("expected the record moved to now, got %q", soon[integrityKey])
	}

	late := fakeBuilt{integrityKey: now.Add(-2 * time.Hour).Format(time.RFC3339)}
	report = aSegfaultCloseOut(t, pane, late, now)
	if strings.Contains(strings.Join(report.Notes, "\n"), "mw cloud down") {
		t.Fatalf("expected a fault two hours after the last not to name the box, got %v", report.Notes)
	}
}

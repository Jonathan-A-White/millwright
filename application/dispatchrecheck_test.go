package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aFoundBug is a bug as mw next files one from a Builder's 'Found, not fixed:'
// line: labelled bug and found-at-close, its Check: line naming a spec, and no
// main sha anywhere in it (mw-gq6.1737).
func aFoundBug(t *testing.T, tracker *apptest.FakeTracker, id string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "[bug] the greek audio spec drops a clip"})
	if err := tracker.SetLabels(id, application.LabelBug, application.LabelFoundAtClose); err != nil {
		t.Fatalf("labelling %s: %v", id, err)
	}
	description := "mw-gq6.1733 (a story) landed, and its closing comment lists this finding under 'Found, not fixed:':\n\n" +
		"> A clip is dropped (src/audio.ts:12)\n\nCheck: " + greekSpec + "\n\nSeen by: mw-gq6.1733\n"
	if err := tracker.SetDescription(id, description); err != nil {
		t.Fatalf("describing %s: %v", id, err)
	}
}

func TestADispatchDoesNotCloseABugWhoseCheckPassedWhenItWasFiled(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	aFoundBug(t, tracker, "mw-gq6.1")
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n"}}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.CloseReason("mw-gq6.1"); got != "" {
		t.Errorf("the bug was closed as %q: its check names no sha, so a pass on main does not show it failed when filed", got)
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Status != apptest.StatusDeferred {
		t.Errorf("the bug's status is %q, want it held (%q)", d.Status, apptest.StatusDeferred)
	}
	said := strings.Join(tracker.Comments("mw-gq6.1"), "\n")
	if !strings.HasPrefix(said, application.HeldCommentPrefix) || !strings.Contains(said, "9f3c2ab41") || strings.Contains(said, "landed after the bug was filed") {
		t.Errorf("the comment should open HELD: and say why, naming the sha, got %q", said)
	}
	if len(report.FixedOnMain) != 0 || len(report.Started) != 0 || len(worktrees.added) != 0 || len(runner.Started()) != 0 {
		t.Errorf("nothing should be closed or started: %+v", report)
	}
}

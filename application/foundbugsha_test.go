package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A bug filed from a finding whose command was seen to fail names the commit it
// failed at, as 'Main's sha:', the line mw suite writes; one whose command was
// never seen to fail names none (mw-gq6.1740).
func TestAFoundBugNamesTheCommitItWasFiledAt(t *testing.T) {
	comment := "Done.\nFound, not fixed:\n" + reproFinding + "\nFor the rig memory: nothing"

	t.Run("a command that fails on main names the commit it failed at", func(t *testing.T) {
		main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: false, Tail: "FAIL"}}
		filed, _, _ := landedWithFindings(t, comment, main)
		if len(filed) != 1 {
			t.Fatalf("expected one bug filed, got %+v", filed)
		}
		if !strings.Contains(filed[0].Description, "\nMain's sha: 21bc605c\n") {
			t.Errorf("expected the description to name the commit the command failed at, got:\n%s", filed[0].Description)
		}
	})

	t.Run("a command that passes at the finder's commit too names none", func(t *testing.T) {
		passes := application.FindingAnswer{Sha: "0123456789ab", Ran: true, Passed: true, Tail: "ok"}
		main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: true, Tail: "ok"}, at: &passes}
		filed, _, _ := landedWithFindings(t, comment, main)
		if len(filed) != 1 {
			t.Fatalf("expected one bug filed, got %+v", filed)
		}
		if strings.Contains(filed[0].Description, "Main's sha") {
			t.Errorf("expected no commit named for a command never seen to fail, got:\n%s", filed[0].Description)
		}
	})

	t.Run("a finding with no command names none", func(t *testing.T) {
		main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: false}}
		filed, _, _ := landedWithFindings(t, "Done.\nFound, not fixed:\nThe parser drops a trailing line (application/parse.go:12). Sweep: none. Size: S. By design? no\nFor the rig memory: nothing", main)
		if len(filed) != 1 || strings.Contains(filed[0].Description, "Main's sha") {
			t.Errorf("expected one bug filed naming no commit, got %+v", filed)
		}
	})
}

// aFoundBugAt is a found-at-close bug whose description names the commit its
// command failed at (mw-gq6.1740).
func aFoundBugAt(t *testing.T, tracker *apptest.FakeTracker, id, sha string) {
	t.Helper()
	aFoundBug(t, tracker, id)
	d, err := tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetDescription(id, d.Description+"\nMain's sha: "+sha+"\n"); err != nil {
		t.Fatal(err)
	}
}

// A found bug that names the commit it failed at is closed as fixed when main
// has moved and its check passes there.
func TestADispatchClosesAFoundBugThatFailedAtAShaWhenMainHasMovedAndItPasses(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aFoundBugAt(t, tracker, "mw-gq6.1", "21bc605c1d9e7a4b5c6d70f83a2b1c0d9e8f7a6b")
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n"}}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.CloseReason("mw-gq6.1"); got == "" {
		t.Errorf("expected the bug closed as fixed: it failed at a sha main has moved past, and passes now")
	}
	if len(report.FixedOnMain) != 1 {
		t.Errorf("expected one story fixed on main, got %+v", report.FixedOnMain)
	}
}

// ... and held, not closed, when main is the very commit it failed at.
func TestADispatchHoldsAFoundBugThatPassesAtTheShaItFailedAt(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	const sha = "21bc605c1d9e7a4b5c6d70f83a2b1c0d9e8f7a6b"
	aFoundBugAt(t, tracker, "mw-gq6.1", sha)
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: sha, Passed: true, Tests: 3, Tail: "  3 passed\n"}}

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.CloseReason("mw-gq6.1"); got != "" {
		t.Errorf("the bug was closed as %q: it passes at the sha it failed at, which is a flake", got)
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Status != apptest.StatusDeferred {
		t.Errorf("the bug's status is %q, want it held (%q)", d.Status, apptest.StatusDeferred)
	}
}

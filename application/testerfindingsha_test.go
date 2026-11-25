package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A Tester's bug filed from a finding whose command was seen to fail names the
// commit it failed at, as 'Main's sha:', the line mw suite and mw next write;
// one whose command was never seen to fail names none (mw-gq6.1742).
func TestATesterFindingNamesTheCommitItFailedAt(t *testing.T) {
	finding := testerFinding()
	finding.Acceptance = "go test ./application -run TestReaderKeepsItsPage passes\nSweep: every viewport effect in src/ is grepped for state it drops"

	file := func(t *testing.T, f application.TesterFinding, main application.FindingRechecker) string {
		t.Helper()
		tracker, rules := testerFileEpic(t)
		filed, err := application.TesterFiler{Tracker: tracker, Rules: rules, Main: main}.File(context.Background(), "mw-e.2", f)
		if err != nil || filed.ID == "" {
			t.Fatalf("expected the finding filed, got %+v, %v", filed, err)
		}
		d, err := tracker.ShowStory(context.Background(), filed.ID)
		if err != nil {
			t.Fatal(err)
		}
		return d.Description
	}

	t.Run("a command that fails on main names the commit it failed at", func(t *testing.T) {
		main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: false, Tail: "FAIL"}}
		if got := file(t, finding, main); !strings.Contains(got, "\nMain's sha: 21bc605c\n") {
			t.Errorf("expected the description to name the commit the command failed at, got:\n%s", got)
		}
	})

	never := map[string]struct {
		change func(*application.TesterFinding)
		main   application.FindingRechecker
	}{
		"a command that passes where the Tester saw the fault too": {func(*application.TesterFinding) {}, &stubFindingChecks{
			answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: true, Tail: "ok"},
			at:     &application.FindingAnswer{Sha: "abc1234", Ran: true, Passed: true, Tail: "ok"}}},
		"a command that did not run on main": {func(*application.TesterFinding) {}, &stubFindingChecks{
			answer: application.FindingAnswer{Sha: "21bc605c", Ran: false}}},
		"a main that could not be read": {func(*application.TesterFinding) {}, &stubFindingChecks{err: errors.New("no tree")}},
		"no main to read":               {func(*application.TesterFinding) {}, nil},
		"no cheap command": {func(f *application.TesterFinding) {
			f.Acceptance = "npm run test:layout passes\nSweep: every viewport effect in src/ is grepped for state it drops"
		}, &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: false}}},
	}
	for name, c := range never {
		t.Run(name+" names none", func(t *testing.T) {
			f := finding
			c.change(&f)
			if got := file(t, f, c.main); strings.Contains(got, "Main's sha") {
				t.Errorf("expected no commit named for a command never seen to fail, got:\n%s", got)
			}
		})
	}
}

// aTesterBug is a bug as mw tester file files one: labelled bug and nothing
// else (no found-at-close), its description carrying the Tester's marker and a
// Check: line naming a spec, and description's tail after that.
func aTesterBug(t *testing.T, tracker *apptest.FakeTracker, id, tail string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{ID: id, Title: "[bug] greek: the audio spec drops a clip"})
	if err := tracker.SetLabels(id, application.LabelBug); err != nil {
		t.Fatalf("labelling %s: %v", id, err)
	}
	description := "Play the greek lesson: a clip is dropped.\n\nCheck: " + greekSpec + "\n\n" + application.TesterFindingMarker +
		". Source: Tester on mw-gq6.1733 (mw-gq6.1734).\n\nHOW TO CHECK IT, for the Governor:\n1. Play it.\n" + tail
	if err := tracker.SetDescription(id, description); err != nil {
		t.Fatalf("describing %s: %v", id, err)
	}
}

// A Tester's bug that names no commit it failed at is held, not closed, when its
// check passes on main: nothing shows it failed when it was filed (mw-gq6.1742).
func TestADispatchHoldsATesterBugThatNamesNoCommitWhenItsCheckPasses(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aTesterBug(t, tracker, "mw-gq6.1", "")
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n"}}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if got := tracker.CloseReason("mw-gq6.1"); got != "" {
		t.Errorf("the bug was closed as %q: it names no commit it failed at, so a pass on main shows no fix", got)
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Status != apptest.StatusDeferred {
		t.Errorf("the bug's status is %q, want it held (%q)", d.Status, apptest.StatusDeferred)
	}
	said := strings.Join(tracker.Comments("mw-gq6.1"), "\n")
	if !strings.HasPrefix(said, application.HeldCommentPrefix) || !strings.Contains(said, "Tester's finding") {
		t.Errorf("the comment should open HELD: and say the bug is a Tester's finding, got %q", said)
	}
	if len(report.FixedOnMain) != 0 {
		t.Errorf("nothing should be closed as fixed: %+v", report.FixedOnMain)
	}
}

// ... and closed as fixed when it names the commit it failed at and main has
// moved past it.
func TestADispatchClosesATesterBugThatFailedAtAShaWhenMainHasMovedAndItPasses(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aTesterBug(t, tracker, "mw-gq6.1", "\nMain's sha: 21bc605c\n")
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

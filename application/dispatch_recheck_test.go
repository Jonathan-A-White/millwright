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

const greekSpec = "tests/e2e/greek-audio.spec.ts"

// stubRechecks is the SpecRechecker a dispatch test gives: it answers what it
// was told to and remembers what it was asked.
type stubRechecks struct {
	answer application.SpecRecheck
	err    error
	asked  []string
}

func (s *stubRechecks) Recheck(_ context.Context, rig, spec string) (application.SpecRecheck, error) {
	s.asked = append(s.asked, rig+" "+spec)
	return s.answer, s.err
}

// aSuiteBug is a ready story of the factory's epic filed the way mw suite --file
// files one: labelled bug and suite, naming the one spec in its files key.
func aSuiteBug(t *testing.T, tracker *apptest.FakeTracker, id string, labels ...string) {
	t.Helper()
	tracker.AddStory("mw-gq6", domain.Story{
		ID:        id,
		Title:     "[bug] lampas main: " + greekSpec + " fails on main",
		Overrides: domain.Path{Files: greekSpec},
	})
	if err := tracker.SetLabels(id, labels...); err != nil {
		t.Fatalf("labelling %s: %v", id, err)
	}
}

func TestADispatchClosesASuiteBugWhoseSpecPassesOnMainAndStartsNoSession(t *testing.T) {
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	recheck := &stubRechecks{answer: application.SpecRecheck{
		Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed (4.1s)\n",
	}}
	dispatch.Rechecks = recheck

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 1 || recheck.asked[0] != "millwright "+greekSpec {
		t.Fatalf("the re-run was asked for %v, want the spec of the bug on its rig", recheck.asked)
	}
	want := "fixed on main before dispatch: " + greekSpec + " passes at 9f3c2ab41 (3 tests)"
	if got := tracker.CloseReason("mw-gq6.1"); got != want {
		t.Errorf("close reason:\n got %q\nwant %q", got, want)
	}
	if said := strings.Join(tracker.Comments("mw-gq6.1"), "\n"); !strings.Contains(said, "3 passed (4.1s)") {
		t.Errorf("the comment should carry the run's tail, got %q", said)
	}
	if len(report.Started) != 0 || len(report.Failed) != 0 {
		t.Errorf("nothing should be started, got %+v", report)
	}
	if len(report.FixedOnMain) != 1 || report.FixedOnMain[0].StoryID != "mw-gq6.1" {
		t.Errorf("the report should name the story closed, got %+v", report.FixedOnMain)
	}
	if d, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); d.Assignee != "" {
		t.Errorf("the story was claimed by %q", d.Assignee)
	}
	if len(worktrees.added) != 0 {
		t.Errorf("a worktree was cut: %v", worktrees.added)
	}
	if sessions := runner.Started(); len(sessions) != 0 {
		t.Errorf("a session was started: %v", sessions)
	}
	if !strings.Contains(report.String(), "fixed on main before dispatch") {
		t.Errorf("the report should say it, got:\n%s", report.String())
	}
}

func TestADispatchStartsASuiteBugWhoseSpecStillFailsOnMain(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "9f3c2ab", Passed: false, Tail: "1 failed"}}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Fatalf("started %v, want the bug dispatched as before", ids)
	}
	if len(report.FixedOnMain) != 0 || tracker.CloseReason("mw-gq6.1") != "" {
		t.Errorf("a failing spec must not close the bug: %+v", report.FixedOnMain)
	}
}

func TestADispatchStartsASuiteBugItCouldNotRerunAndSaysWhy(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	dispatch.Rechecks = &stubRechecks{err: errors.New("cutting a worktree of millwright: no tree")}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Fatalf("started %v, want the bug dispatched as before", ids)
	}
	if !strings.Contains(report.Started[0].Recheck, "no tree") {
		t.Errorf("the started story should carry why it was not re-run, got %q", report.Started[0].Recheck)
	}
	if !strings.Contains(report.String(), "not re-run on main first: ") || !strings.Contains(report.String(), "no tree") {
		t.Errorf("the dispatch line should say so, got:\n%s", report.String())
	}
	if tracker.CloseReason("mw-gq6.1") != "" {
		t.Errorf("a re-run that could not be made must not close the bug")
	}
}

func TestADispatchNeverRerunsAStoryThatIsNotASuiteBugOrNamesMoreThanOneSpec(t *testing.T) {
	for name, story := range map[string]struct {
		labels []string
		files  string
	}{
		"no suite label":   {[]string{application.LabelBug}, greekSpec},
		"no label at all":  {nil, greekSpec},
		"two files":        {[]string{application.LabelBug, application.LabelSuite}, greekSpec + ",tests/e2e/b.spec.ts"},
		"a glob":           {[]string{application.LabelBug, application.LabelSuite}, "tests/e2e/*.spec.ts"},
		"not a test file":  {[]string{application.LabelBug, application.LabelSuite}, "src/audio.ts"},
		"no files at all":  {[]string{application.LabelBug, application.LabelSuite}, ""},
		"a flaky bug":      {[]string{application.LabelBug, application.LabelSuite}, greekSpec},
		"a spec with text": {[]string{application.LabelBug, application.LabelSuite}, greekSpec + " and more"},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			title := "A story"
			if name == "a flaky bug" {
				title = "[bug] lampas main: " + greekSpec + " is flaky on main (failed, then passed alone)"
			}
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: title, Overrides: domain.Path{Files: story.files}})
			if err := tracker.SetLabels("mw-gq6.1", story.labels...); err != nil {
				t.Fatal(err)
			}
			recheck := &stubRechecks{answer: application.SpecRecheck{Passed: true}}
			dispatch.Rechecks = recheck

			report, err := dispatch.Run(context.Background())
			if err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(recheck.asked) != 0 {
				t.Errorf("the re-run was asked for %v, want none", recheck.asked)
			}
			if ids := startedIDs(report); len(ids) != 1 {
				t.Errorf("started %v, want the story dispatched as before", ids)
			}
		})
	}
}

func TestADryRunDoesNotRerunASuiteBug(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	recheck := &stubRechecks{answer: application.SpecRecheck{Passed: true}}
	dispatch.Rechecks = recheck
	dispatch.DryRun = true

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 0 || len(report.FixedOnMain) != 0 {
		t.Errorf("a dry run re-ran %v and closed %v", recheck.asked, report.FixedOnMain)
	}
}

// A closed-before-dispatch story takes none of the sessions the cap counts.
func TestAStoryClosedBeforeDispatchTakesNoSlotOfTheCap(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "Another story"})
	dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "abc", Passed: true, Tests: 1}}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.2" {
		t.Fatalf("started %v, want mw-gq6.2 to take the one slot", ids)
	}
}

func recheckSuite(runs *apptest.FakeSuiteRunner) (application.Suite, *apptest.FakeSuiteTrees) {
	heads := apptest.NewFakeRigHeads()
	heads.SetHead("/rigs/lampas", "9f3c2ab41d0e")
	trees := &apptest.FakeSuiteTrees{}
	return application.Suite{
		Trees:  trees,
		Heads:  heads,
		Runner: runs,
		Rigs:   map[string]string{"lampas": "/rigs/lampas"},
		Tests:  map[string]string{"lampas": "npm test"},
		ChangedSpecs: map[string]string{
			"lampas": "npx playwright test " + application.SpecsPlaceholder,
		},
	}, trees
}

func TestASuiteRechecksOneSpecAloneOnMainWithTheCommandItRunsFilesWith(t *testing.T) {
	runs := &apptest.FakeSuiteRunner{Respond: func(_, _ string) (application.Checked, error) {
		return application.Checked{Passed: true, Output: "Running 3 tests using 1 worker\n  3 passed (4.1s)\n"}, nil
	}}
	suite, trees := recheckSuite(runs)

	got, err := suite.Recheck(context.Background(), "lampas", greekSpec)
	if err != nil {
		t.Fatalf("re-running: %v", err)
	}
	if !got.Passed || got.Tests != 3 || got.Sha != "9f3c2ab41d0e" || got.Spec != greekSpec {
		t.Errorf("got %+v, want a pass of 3 tests at the main sha", got)
	}
	if !strings.Contains(got.Tail, "3 passed") {
		t.Errorf("the tail should hold the end of the run, got %q", got.Tail)
	}
	ran := runs.Ran()
	if len(ran) != 1 || ran[0].Command != "npx playwright test "+greekSpec {
		t.Fatalf("ran %+v, want the changed-specs command with just the spec", ran)
	}
	if len(trees.Added()) != 1 || trees.Live() != 0 {
		t.Errorf("one worktree should be cut and taken away, added %v live %d", trees.Added(), trees.Live())
	}
}

func TestASuiteRechecksAnUnitTestFileWithTheTestsCommand(t *testing.T) {
	runs := &apptest.FakeSuiteRunner{}
	suite, _ := recheckSuite(runs)
	if _, err := suite.Recheck(context.Background(), "lampas", "src/dates.test.ts"); err != nil {
		t.Fatal(err)
	}
	if ran := runs.Ran(); len(ran) != 1 || ran[0].Command != "npm test src/dates.test.ts" {
		t.Errorf("ran %+v, want the tests command with the file added", ran)
	}
}

func TestASuiteRecheckOfASpecThatFailsIsNotAPassAndOneThatCannotRunIsAnError(t *testing.T) {
	failing := &apptest.FakeSuiteRunner{Respond: func(_, _ string) (application.Checked, error) {
		return application.Checked{Passed: false, Output: "  1) [shots] › " + greekSpec + ":4:1 › plays\n  1 failed\n"}, nil
	}}
	suite, _ := recheckSuite(failing)
	got, err := suite.Recheck(context.Background(), "lampas", greekSpec)
	if err != nil || got.Passed {
		t.Errorf("a failing spec is an answer, not an error: %+v, %v", got, err)
	}

	notRun := &apptest.FakeSuiteRunner{Respond: func(_, _ string) (application.Checked, error) {
		return application.Checked{NotRun: true, Output: "sh: npx: not found"}, nil
	}}
	suite, _ = recheckSuite(notRun)
	if _, err := suite.Recheck(context.Background(), "lampas", greekSpec); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("a command that could not be run is an error saying why, got %v", err)
	}

	suite, _ = recheckSuite(&apptest.FakeSuiteRunner{})
	if _, err := suite.Recheck(context.Background(), "nosuchrig", greekSpec); err == nil {
		t.Errorf("a rig this host does not have is an error")
	}
}

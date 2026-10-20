package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	// lock, when it is set, is the gate lock the re-run is watched under: held
	// holds how many locks were out when each re-run began.
	lock *aGateLock
	held []int
}

func (s *stubRechecks) Recheck(_ context.Context, rig, spec string) (application.SpecRecheck, error) {
	s.asked = append(s.asked, rig+" "+spec)
	if s.lock != nil {
		s.held = append(s.held, s.lock.taken-s.lock.freed)
	}
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

// aBusyGateLock is a host gate lock another gate holds for as long as the
// asker is willing to wait.
type aBusyGateLock struct{ by string }

func (b aBusyGateLock) Take(ctx context.Context, _ string, held func(string)) (application.GateTurn, error) {
	if held != nil {
		held(b.by)
	}
	<-ctx.Done()
	return application.GateTurn{}, ctx.Err()
}

func TestTheSuiteBugRecheckRunsOnlyWhileTheGateLockIsHeldAndGivesItBackWhateverItFinds(t *testing.T) {
	for name, answer := range map[string]struct {
		found application.SpecRecheck
		err   error
	}{
		"a pass":       {found: application.SpecRecheck{Spec: greekSpec, Sha: "abc", Passed: true, Tests: 1}},
		"a fail":       {found: application.SpecRecheck{Spec: greekSpec, Sha: "abc", Passed: false}},
		"a cannot-run": {err: errors.New("no tree")},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, _, _ := aFactory(t)
			aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
			lock := &aGateLock{}
			recheck := &stubRechecks{answer: answer.found, err: answer.err, lock: lock}
			dispatch.Rechecks = recheck
			dispatch.GateLock = lock

			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			if len(recheck.held) != 1 || recheck.held[0] != 1 {
				t.Errorf("the re-run should begin with the gate lock held once, got %v", recheck.held)
			}
			if lock.taken != 1 || lock.freed != 1 {
				t.Errorf("the lock should be taken once and given back, got taken %d freed %d", lock.taken, lock.freed)
			}
			if lock.holders[0] != "mw-gq6.1" {
				t.Errorf("the lock should be taken for the story, got %v", lock.holders)
			}
		})
	}
}

func TestASuiteBugIsNotRerunWhileAnotherGateHoldsTheLockAndIsDispatchedAsBefore(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	recheck := &stubRechecks{answer: application.SpecRecheck{Spec: greekSpec, Sha: "abc", Passed: true, Tests: 1}}
	dispatch.Rechecks = recheck
	dispatch.GateLock = aBusyGateLock{by: "mw-x.9"}
	dispatch.GateWait = time.Millisecond

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 0 {
		t.Errorf("the re-run was made without the gate lock: %v", recheck.asked)
	}
	if ids := startedIDs(report); len(ids) != 1 || ids[0] != "mw-gq6.1" {
		t.Fatalf("started %v, want the bug dispatched as before", ids)
	}
	if tracker.CloseReason("mw-gq6.1") != "" {
		t.Errorf("a bug that was not re-run must not be closed")
	}
	if !strings.Contains(report.Started[0].Recheck, "not re-checked: the gate is busy") {
		t.Errorf("the started story should say the gate was busy, got %q", report.Started[0].Recheck)
	}
	if !strings.Contains(report.String(), "not re-checked: the gate is busy") {
		t.Errorf("the dispatch line should say so, got:\n%s", report.String())
	}
}

func TestASuiteBugIsDispatchedWhenTheGateLockCannotBeTakenForAnotherReason(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
	recheck := &stubRechecks{answer: application.SpecRecheck{Passed: true}}
	dispatch.Rechecks = recheck
	dispatch.GateLock = &aGateLock{refuse: errors.New("no room for the lock file")}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if len(recheck.asked) != 0 || len(startedIDs(report)) != 1 {
		t.Fatalf("re-ran %v, started %v", recheck.asked, startedIDs(report))
	}
	if got := report.Started[0].Recheck; !strings.Contains(got, "not re-checked") || !strings.Contains(got, "no room for the lock file") {
		t.Errorf("the line should say why, got %q", got)
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

// A re-run is made in a worktree just cut from main, which has no node_modules:
// a changed-specs command that builds first (lampas's) finds no tsc there. The
// runner below is as honest as that: the dependencies are there only when the
// command installs them before it builds (mw-gq6.638).
func TestASuiteRecheckInstallsTheRigsDependenciesBeforeItBuildsAndRunsTheSpec(t *testing.T) {
	runs := &apptest.FakeSuiteRunner{Respond: func(_, command string) (application.Checked, error) {
		install, build := strings.Index(command, "npm ci"), strings.Index(command, "npm run build")
		if build >= 0 && (install < 0 || install > build) {
			return application.Checked{NotRun: true, Output: "> tsc -b && vite build\nsh: 1: tsc: not found\n"}, nil
		}
		return application.Checked{Passed: true, Output: "  2 passed (6.0s)\n"}, nil
	}}
	suite, _ := recheckSuite(runs)
	suite.Tests["lampas"] = "npm ci --no-audit --no-fund && TZ=UTC npm test && npm run typecheck && npm run build"
	suite.ChangedSpecs["lampas"] = "npm run build && TZ=UTC npx playwright test --project=shots " + application.SpecsPlaceholder

	got, err := suite.Recheck(context.Background(), "lampas", greekSpec)
	if err != nil || !got.Passed || got.Tests != 2 {
		t.Fatalf("got %+v, %v, want a pass of 2 tests", got, err)
	}
	ran := runs.Ran()
	want := "npm ci --no-audit --no-fund && npm run build && TZ=UTC npx playwright test --project=shots " + greekSpec
	if len(ran) != 1 || ran[0].Command != want {
		t.Errorf("ran %+v, want the install, then the changed-specs command: %s", ran, want)
	}
}

func TestASuiteRecheckOfAnUnitTestFileAddsNoSecondInstall(t *testing.T) {
	runs := &apptest.FakeSuiteRunner{}
	suite, _ := recheckSuite(runs)
	suite.Tests["lampas"] = "npm ci --no-audit --no-fund && npm test"
	if _, err := suite.Recheck(context.Background(), "lampas", "src/dates.test.ts"); err != nil {
		t.Fatal(err)
	}
	if ran := runs.Ran(); len(ran) != 1 || ran[0].Command != "npm ci --no-audit --no-fund && npm test src/dates.test.ts" {
		t.Errorf("ran %+v, want the tests command with the file added and one install", ran)
	}
}

func TestASuiteRecheckOfARigWhoseTestsInstallNothingRunsTheSpecCommandAsItIs(t *testing.T) {
	runs := &apptest.FakeSuiteRunner{}
	suite, _ := recheckSuite(runs)
	suite.Tests["lampas"] = "make test"
	if _, err := suite.Recheck(context.Background(), "lampas", greekSpec); err != nil {
		t.Fatal(err)
	}
	if ran := runs.Ran(); len(ran) != 1 || ran[0].Command != "npx playwright test "+greekSpec {
		t.Errorf("ran %+v, want the changed-specs command alone", ran)
	}
}

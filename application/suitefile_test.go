package application_test

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestASuiteBugFiledIntoTheSuiteEpicIsANeedOfTheEpicsDemo(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-s", domain.Path{Rig: "lampas"})
	tracker.AddStory("mw-s", domain.Story{ID: "mw-s.1", Title: "Show the suite"})
	if err := tracker.SetLabels("mw-s.1", application.LabelDemo); err != nil {
		t.Fatal(err)
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{LastStoryLabels: []string{"demo"}, SuiteEpic: "mw-s"})
	report := application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234",
		Failing: []application.SuiteFile{{File: "src/a.test.ts"}}}}

	filer := application.SuiteFiler{Tracker: tracker, Rules: rules, Mailbox: apptest.NewFakeMailbox(), Host: "desktop"}
	filed, err := filer.File(context.Background(), "mw-s", report)
	if err != nil || len(filed) != 1 || !filed[0].New {
		t.Fatalf("expected one new bug, got %+v, %v", filed, err)
	}
	epic, err := tracker.ShowEpic(context.Background(), "mw-s")
	if err != nil {
		t.Fatal(err)
	}
	for _, story := range epic.Stories {
		if story.Story.ID == "mw-s.1" && !slices.Contains(story.Needs, filed[0].ID) {
			t.Errorf("expected the demo to wait on the bug %s, got %q", filed[0].ID, story.Needs)
		}
	}
}

// TestASuiteResultKeepsTheCommandAndSecondsOfEachCommandLineItRan: the rule
// that a close-out waits behind one command line of the suite (mw-gq6.917) can
// be read from the logs only if the result keeps how long each line ran
// (mw-gq6.920): the tests line, the changed specs and each re-run alone, in the
// order they ran, the seconds being the run and not the wait for the gate lock.
func TestASuiteResultKeepsTheCommandAndSecondsOfEachCommandLineItRan(t *testing.T) {
	failing := "Running 2 tests using 1 worker\n" +
		"  1) [shots] › tests/e2e/teach.spec.ts:4:1 › Words lists a word ──────\n\n    Error: timed out\n\n" +
		"  1 failed\n  1 passed (9.0m)\n"
	suite, records, _, _ := lampasSuite(t, map[string]application.Checked{
		"npm test":             {Passed: true, Output: "ok\n"},
		"npx playwright test ": {Output: failing},
		"npx playwright test tests/e2e/teach.spec.ts": {Passed: true, Output: "ok\n"},
	})
	clock := time.Date(2026, 10, 19, 8, 0, 1, 0, time.UTC)
	suite.Now = func() time.Time { return clock }
	took := map[string]time.Duration{
		"npm test":             40*time.Minute + 30*time.Second,
		"npx playwright test ": 9 * time.Minute,
		"npx playwright test tests/e2e/teach.spec.ts": 12*time.Second + 500*time.Millisecond,
	}
	ran := suite.Runner.(*apptest.FakeSuiteRunner)
	answer := ran.Respond
	ran.Respond = func(dir, command string) (application.Checked, error) {
		clock = clock.Add(took[command])
		return answer(dir, command)
	}

	report, err := suite.Run(context.Background(), "lampas")
	if err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	want := []application.SuiteLine{
		{Command: "npm test", Seconds: 2430},
		{Command: "npx playwright test ", Seconds: 540},
		{Command: "npx playwright test tests/e2e/teach.spec.ts", Seconds: 12.5},
	}
	if !reflect.DeepEqual(report.Result.Lines, want) {
		t.Errorf("lines of the result: got %+v, want %+v", report.Result.Lines, want)
	}

	raw, _ := records.File("runs/suite/lampas/2026-10-19.json")
	var kept struct {
		Lines []struct {
			Command string  `json:"command"`
			Seconds float64 `json:"seconds"`
		} `json:"lines"`
	}
	if err := json.Unmarshal([]byte(raw), &kept); err != nil || len(kept.Lines) != 3 ||
		kept.Lines[0].Command != "npm test" || kept.Lines[0].Seconds != 2430 {
		t.Errorf("expected the kept result to carry each line's command and seconds, got %+v, %v\n%s", kept.Lines, err, raw)
	}
}

// TestAnOldSuiteResultWithNoLinesIsReadAndAnEmptyOneIsNotWritten: the result
// files kept before mw-gq6.920 have no lines; they are read as a result with
// none, and a result with none writes no key for them.
func TestAnOldSuiteResultWithNoLinesIsReadAndAnEmptyOneIsNotWritten(t *testing.T) {
	old := `{"rig":"lampas","host":"laptop","sha":"abc","branch":"main","started":"2026-10-13T08:00:01Z",` +
		`"duration_seconds":2160,"waited_seconds":0,"gave_up_waiting":false,"commands":["npm test"],` +
		`"passed":true,"failing_files":[]}`
	record, err := application.ParseSuiteRecord("2026-10-13", old)
	if err != nil {
		t.Fatalf("reading an old result: %v", err)
	}
	if len(record.Result.Lines) != 0 || !record.Result.Passed || record.Result.DurationSeconds != 2160 {
		t.Errorf("expected the old result read with no lines, got %+v", record.Result)
	}
	written, err := json.Marshal(record.Result)
	if err != nil || strings.Contains(string(written), `"lines"`) {
		t.Errorf("expected no lines key for a result with none, got %s, %v", written, err)
	}
}

// suiteHostFiling sets up the filing of a lampas run on host: lampas names desktop
// as its suite host, or nothing of one when onSuiteHost is false.
func suiteHostFiling(t *testing.T, host string, withSuiteHost bool) (application.SuiteFiler, *apptest.FakeTracker, *apptest.FakeMailbox) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-s", domain.Path{Rig: "lampas"})
	rules := apptest.NewFakeEpicRules()
	req := domain.EpicRequirements{NightlySuite: true, SuiteEpic: "mw-s"}
	if withSuiteHost {
		req.SuiteEvery, req.SuiteHost = 6*time.Hour, "desktop"
	}
	rules.Require("lampas", req)
	mailbox := apptest.NewFakeMailbox()
	return application.SuiteFiler{Tracker: tracker, Rules: rules, Mailbox: mailbox, Host: host}, tracker, mailbox
}

func flakyAndFailing() application.SuiteReport {
	return application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Failing: []application.SuiteFile{
		{File: "tests/e2e/flaky.spec.ts", Alone: "passed", Flaky: true},
		{File: "tests/e2e/broken.spec.ts", Alone: "failed"},
	}}}
}

func TestARunOnTheSuiteHostFilesNoNewBugForAFileThatPassedAloneButFilesTheOthers(t *testing.T) {
	filer, tracker, mailbox := suiteHostFiling(t, "desktop", true)
	filed, err := filer.File(context.Background(), "mw-s", flakyAndFailing())
	if err != nil {
		t.Fatal(err)
	}
	if len(filed) != 1 || filed[0].File != "tests/e2e/broken.spec.ts" || !filed[0].New {
		t.Fatalf("expected one new bug, for the file that failed twice, got %+v", filed)
	}
	epic, _ := tracker.ShowEpic(context.Background(), "mw-s")
	if len(epic.Stories) != 1 {
		t.Errorf("expected one bug under the epic, got %d", len(epic.Stories))
	}
	if mailbox.Writes() != 1 {
		t.Errorf("a run that filed a new bug mails the Mayor once, got %d mails", mailbox.Writes())
	}
}

func TestARunOnTheSuiteHostStillCommentsOnTheOpenBugOfAFlakyFile(t *testing.T) {
	filer, tracker, mailbox := suiteHostFiling(t, "desktop", true)
	id, err := tracker.CreateStory(context.Background(), application.NewStory{
		EpicID: "mw-s", Title: "[bug] lampas main: tests/e2e/flaky.spec.ts fails on main", Labels: []string{application.LabelBug, application.LabelSuite},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Failing: []application.SuiteFile{
		{File: "tests/e2e/flaky.spec.ts", Alone: "passed", Flaky: true},
	}}}
	filed, err := filer.File(context.Background(), "mw-s", report)
	if err != nil || len(filed) != 1 || filed[0].ID != id || filed[0].New {
		t.Fatalf("expected a comment on %s, got %+v, %v", id, filed, err)
	}
	if mailbox.Writes() != 0 {
		t.Errorf("a run that filed nothing new sends no mail, got %d", mailbox.Writes())
	}
}

func TestARunOnTheSuiteHostThatFiledNothingNewSendsNoMailWhetherItPassedOrNot(t *testing.T) {
	filer, _, mailbox := suiteHostFiling(t, "desktop", true)
	for name, report := range map[string]application.SuiteReport{
		"passed": {Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Passed: true}},
		"flaky only": {Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Failing: []application.SuiteFile{
			{File: "tests/e2e/flaky.spec.ts", Alone: "passed", Flaky: true}}}},
	} {
		if _, err := filer.File(context.Background(), "mw-s", report); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if mailbox.Writes() != 0 {
		t.Errorf("expected no mail, got %d", mailbox.Writes())
	}
}

func TestTheHomesNightlyFilesAFlakyFileAndMailsAsBefore(t *testing.T) {
	for name, withSuiteHost := range map[string]bool{"a rig with no suite host": false, "the home, which is not the suite host": true} {
		filer, _, mailbox := suiteHostFiling(t, "laptop", withSuiteHost)
		filed, err := filer.File(context.Background(), "mw-s", flakyAndFailing())
		if err != nil || len(filed) != 2 || !filed[0].New || !filed[1].New {
			t.Fatalf("%s: expected two new bugs, flaky one too, got %+v, %v", name, filed, err)
		}
		if mailbox.Writes() != 1 {
			t.Errorf("%s: expected one mail, got %d", name, mailbox.Writes())
		}
		clean, _, quiet := suiteHostFiling(t, "laptop", withSuiteHost)
		if _, err := clean.File(context.Background(), "mw-s", application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Passed: true}}); err != nil || quiet.Writes() != 1 {
			t.Errorf("%s: a pass is mailed as before, got %d mails, %v", name, quiet.Writes(), err)
		}
	}
}

// foldingStory puts under the suite epic a story that is no suite bug (no label,
// a title that names no file) but whose files key names specs, as the one that
// folded the per-file bugs into a single fix does (mw-gq6.1548).
func foldingStory(t *testing.T, tracker *apptest.FakeTracker, status, files string) string {
	t.Helper()
	tracker.AddStory("mw-s", domain.Story{ID: "mw-s.fold", Title: "One fix, four bugs folded", Overrides: domain.Path{Files: files}})
	if err := tracker.SetStatus("mw-s.fold", status); err != nil {
		t.Fatal(err)
	}
	return "mw-s.fold"
}

func TestAFailingSpecNamedInTheFilesKeyOfAStoryThatIsNotClosedIsCommentedOnNotFiledAgain(t *testing.T) {
	for _, status := range []string{application.StatusInProgress, application.StatusOpen, application.StatusHeld} {
		t.Run(status, func(t *testing.T) {
			filer, tracker, _ := suiteHostFiling(t, "laptop", false)
			id := foldingStory(t, tracker, status, "tests/e2e/open-in-app.spec.ts, tests/e2e/tutor-door.spec.ts")
			report := application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Failing: []application.SuiteFile{
				{File: "tests/e2e/open-in-app.spec.ts", Alone: "failed"},
				{File: "tests/e2e/tutor-door.spec.ts", Alone: "failed"},
			}}}
			filed, err := filer.File(context.Background(), "mw-s", report)
			if err != nil || len(filed) != 2 {
				t.Fatalf("expected two results, got %+v, %v", filed, err)
			}
			for _, one := range filed {
				if one.New || one.ID != id {
					t.Errorf("expected a comment on %s for %s, got %+v", id, one.File, one)
				}
			}
			if epic, _ := tracker.ShowEpic(context.Background(), "mw-s"); len(epic.Stories) != 1 {
				t.Errorf("expected no new bead under the epic, got %d stories", len(epic.Stories))
			}
			if got := tracker.Comments(id); len(got) != 2 || !strings.Contains(got[0], "tests/e2e/open-in-app.spec.ts") {
				t.Errorf("expected a dated comment for each spec on %s, got %q", id, got)
			}
		})
	}
}

func TestAFailingSpecNamedInTheFilesKeyOfAClosedStoryOrNamedOnlyByAGlobIsFiledAsBefore(t *testing.T) {
	for name, story := range map[string][2]string{
		"closed":    {application.StatusClosed, "tests/e2e/a.spec.ts"},
		"a glob":    {application.StatusInProgress, "tests/e2e/*.spec.ts"},
		"a sibling": {application.StatusInProgress, "tests/e2e/a.spec.ts.snap"},
	} {
		t.Run(name, func(t *testing.T) {
			filer, tracker, _ := suiteHostFiling(t, "laptop", false)
			foldingStory(t, tracker, story[0], story[1])
			report := application.SuiteReport{Result: application.SuiteResult{Rig: "lampas", Sha: "abc1234", Failing: []application.SuiteFile{
				{File: "tests/e2e/a.spec.ts", Alone: "failed"}}}}
			filed, err := filer.File(context.Background(), "mw-s", report)
			if err != nil || len(filed) != 1 || !filed[0].New {
				t.Fatalf("expected a new bug, got %+v, %v", filed, err)
			}
		})
	}
}

// movedSuite is the lampas suite of the run of 2026-11-22 (mw-gq6.1654): the
// whole run of `npm test` fails src/dates.test.ts and the file fails alone in
// the run's own tree, and when moves is set main has moved on to newSha by the time
// the run ends. The file's run alone in a recheck tree, at the head it was cut
// at, says atHead, and each such run is counted.
func movedSuite(t *testing.T, moves bool, atHead application.Checked) (application.Suite, *apptest.FakeTracker, *apptest.FakeMailbox, *int) {
	t.Helper()
	const file = "src/dates.test.ts"
	failed := "FAIL  " + file + " > parses dates\nAssertionError: expected 1 to be 2\n"
	suite, _, tracker, mailbox := lampasSuite(t, nil)
	heads := suite.Heads.(*apptest.FakeRigHeads)
	again := 0
	suite.Runner.(*apptest.FakeSuiteRunner).Respond = func(dir, command string) (application.Checked, error) {
		switch {
		case strings.Contains(dir, application.RecheckTreePrefix):
			again++
			return atHead, nil
		case command == "npm test":
			if moves {
				heads.SetHead("/rigs/lampas", "82a89e71c")
			}
			return application.Checked{Output: failed}, nil
		case command == "npm test -- "+file:
			return application.Checked{Output: failed}, nil
		case command == "npx playwright test ":
			return application.Checked{Passed: true}, nil
		}
		t.Errorf("the suite ran %q, which the test did not expect", command)
		return application.Checked{Passed: true}, nil
	}
	return suite, tracker, mailbox, &again
}

func mailBody(t *testing.T, mailbox *apptest.FakeMailbox) string {
	t.Helper()
	mail, _ := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(mail) != 1 {
		t.Fatalf("expected one mail to the Mayor, got %d", len(mail))
	}
	return mail[0].Body
}

func TestAFileThatPassesAloneAtTheNewHeadOfMainIsNotFiledAndTheMailSaysSo(t *testing.T) {
	suite, tracker, mailbox, again := movedSuite(t, true, application.Checked{Passed: true, Output: "ok\n"})
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	if *again != 1 {
		t.Errorf("expected the file run alone once at the new head, got %d runs", *again)
	}
	if stories := tracker.Stories(); len(stories) != 0 {
		t.Errorf("expected no bug filed for a file that passes at the new head, got %v", stories)
	}
	body := mailBody(t, mailbox)
	for _, want := range []string{"src/dates.test.ts", "passed at 82a89e71c (main moved since 11541bc0d), not filed"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected the mail to say %q, got:\n%s", want, body)
		}
	}
}

func TestAFileThatFailsAloneAtTheNewHeadOfMainIsFiledNamingBothShas(t *testing.T) {
	failed := "FAIL  src/dates.test.ts > parses dates\nAssertionError: expected 1 to be 2\n"
	suite, tracker, _, again := movedSuite(t, true, application.Checked{Output: failed})
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	if *again != 1 {
		t.Errorf("expected one run alone at the new head, got %d", *again)
	}
	stories := tracker.Stories()
	if len(stories) != 1 {
		t.Fatalf("expected one bug filed, got %v", stories)
	}
	story, _ := tracker.ShowStory(context.Background(), stories[0])
	for _, want := range []string{"11541bc0d", "82a89e71c"} {
		if !strings.Contains(story.Description, want) {
			t.Errorf("expected the bug to name %s, got:\n%s", want, story.Description)
		}
	}
}

func TestNoSecondRunAloneWhenMainHasNotMoved(t *testing.T) {
	suite, tracker, mailbox, again := movedSuite(t, false, application.Checked{Passed: true})
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	if *again != 0 {
		t.Errorf("expected no run at a new head when main has not moved, got %d", *again)
	}
	if stories := tracker.Stories(); len(stories) != 1 {
		t.Errorf("expected the bug filed as today, got %v", stories)
	}
	if body := mailBody(t, mailbox); strings.Contains(body, "not filed") {
		t.Errorf("expected nothing said of a file not filed, got:\n%s", body)
	}
}

func TestAFileWithAnOpenBugIsCommentedOnWithNoRunAtTheNewHead(t *testing.T) {
	suite, tracker, _, again := movedSuite(t, true, application.Checked{Passed: true})
	tracker.AddStory("mw-suite", domain.Story{ID: "mw-suite.1", Title: "[bug] lampas main: src/dates.test.ts fails on main"})
	if err := tracker.SetLabels("mw-suite.1", application.LabelBug, application.LabelSuite); err != nil {
		t.Fatal(err)
	}
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	if *again != 0 {
		t.Errorf("expected no run at the new head for a file with an open bug, got %d", *again)
	}
	if comments := tracker.Comments("mw-suite.1"); len(comments) != 1 {
		t.Errorf("expected the open bug commented on once, got %v", comments)
	}
	if len(tracker.Stories()) != 1 {
		t.Errorf("expected no new bug, got %v", tracker.Stories())
	}
}

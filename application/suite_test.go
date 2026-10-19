package application_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestFailingFilesNamesEachFileOnceWithItsFirstTestAndError(t *testing.T) {
	vitest := "\x1b[31m FAIL \x1b[39m  src/dates.test.ts > parses dates > in UTC\n" +
		"AssertionError: expected 1 to be 2\n" +
		" ❯ src/dates.test.ts:12:5\n\n" +
		" FAIL  src/dates.test.ts > formats dates\n" +
		"Error: later\n" +
		" FAIL  src/config.test.ts\n" +
		"Error: suite failed to run\n"
	got := application.FailingFiles(vitest)
	want := []application.FailedFile{
		{File: "src/dates.test.ts", Test: "parses dates > in UTC", Error: "AssertionError: expected 1 to be 2\n❯ src/dates.test.ts:12:5"},
		{File: "src/config.test.ts", Error: "Error: suite failed to run"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vitest: got %+v\nwant %+v", got, want)
	}

	playwright := "Running 3 tests using 1 worker\n" +
		"  ✓  1 [shots] › tests/e2e/a.spec.ts:3:1 › fine (1.0s)\n" +
		"  ✘  2 [shots] › tests/e2e/immersive-reader.spec.ts:44:5 › shows the group (2.1s)\n\n\n" +
		"  1) [shots] › tests/e2e/immersive-reader.spec.ts:44:5 › shows the group ──────\n\n" +
		"    Error: expect(locator).toBeVisible()\n\n" +
		"    Locator: getByText('x')\n"
	got = application.FailingFiles(playwright)
	want = []application.FailedFile{{
		File:  "tests/e2e/immersive-reader.spec.ts",
		Test:  "shows the group",
		Error: "Error: expect(locator).toBeVisible()\nLocator: getByText('x')",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("playwright: got %+v\nwant %+v", got, want)
	}

	// The line and dot reporters print no progress lines, only the numbered headers.
	got = application.FailingFiles("  1) [shots] › tests/e2e/b.spec.ts:9:1 › the title ──────\n\n    Error: nope\n")
	want = []application.FailedFile{{File: "tests/e2e/b.spec.ts", Test: "the title", Error: "Error: nope"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("playwright headers: got %+v\nwant %+v", got, want)
	}

	if got := application.FailingFiles("--- FAIL: TestX (0.00s)\nFAIL\n"); len(got) != 0 {
		t.Errorf("a go test failure names no file, got %+v", got)
	}
}

// lampasSuite is a suite of the rig lampas on fakes whose runner answers each
// command line from answers, and which files under the epic mw-suite.
func lampasSuite(t *testing.T, answers map[string]application.Checked) (application.Suite, *apptest.FakeSuiteRecords, *apptest.FakeTracker, *apptest.FakeMailbox) {
	t.Helper()
	heads := apptest.NewFakeRigHeads()
	heads.SetHead("/rigs/lampas", "11541bc0d")
	tracker, mailbox, rules := apptest.NewFakeTracker(), apptest.NewFakeMailbox(), apptest.NewFakeEpicRules()
	tracker.AddEpic("mw-suite", domain.Path{Rig: "lampas", Branch: "main"})
	rules.Require("lampas", domain.EpicRequirements{NightlySuite: true, SuiteEpic: "mw-suite"})
	records := &apptest.FakeSuiteRecords{}
	runner := &apptest.FakeSuiteRunner{Respond: func(_, command string) (application.Checked, error) {
		if answer, ok := answers[command]; ok {
			return answer, nil
		}
		t.Errorf("the suite ran %q, which the test did not expect", command)
		return application.Checked{Passed: true}, nil
	}}
	suite := application.Suite{
		Trees: &apptest.FakeSuiteTrees{}, Heads: heads, Runner: runner, Records: records,
		Now:          func() time.Time { return time.Date(2026, 10, 19, 8, 0, 1, 0, time.UTC) },
		Host:         "laptop",
		Rigs:         map[string]string{"lampas": "/rigs/lampas"},
		Tests:        map[string]string{"lampas": "npm test"},
		ChangedSpecs: map[string]string{"lampas": "npx playwright test {specs}"},
		Filer:        &application.SuiteFiler{Tracker: tracker, Rules: rules, Mailbox: mailbox, Host: "laptop"},
	}
	return suite, records, tracker, mailbox
}

// TestARecheckThatRunsNoTestIsVoidAndNotFiledAsABug: the Lampas nightly suite of
// 2026-10-19 filed four bugs whose re-runs alone never ran a test: each died on
// the preview server's port being used (mw-gq6.625). A re-run that fails
// without running the file's test, by a port in use or a build that fails, is
// recheck void: said so in the summary and the mail, and filed as no bug; a
// file that fails again alone is still filed.
func TestARecheckThatRunsNoTestIsVoidAndNotFiledAsABug(t *testing.T) {
	whole := "Running 580 tests using 4 workers\n" +
		"  1) [shots] › tests/e2e/ipa-course.spec.ts:9:1 › the Optional entry fits the phone ──────\n\n    Error: expect(locator).toBeVisible()\n\n" +
		"  2) [shots] › tests/e2e/teach.spec.ts:4:1 › Words lists a word ──────\n\n    Error: timed out\n\n" +
		"  3) [shots] › tests/e2e/unlock-room.spec.ts:7:1 › Licence page fits ──────\n\n    Error: nope\n\n" +
		"  3 failed\n  577 passed (21.0m)\n"
	suite, records, tracker, mailbox := lampasSuite(t, map[string]application.Checked{
		"npm test":             {Passed: true, Output: "ok\n"},
		"npx playwright test ": {Output: whole},
		"npx playwright test tests/e2e/ipa-course.spec.ts": {Output: "> vite build\n✓ built in 5.78s\n" +
			"Error: http://localhost:5142 is already used, make sure that nothing is running on the port/url or set reuseExistingServer:true in config.webServer.\n"},
		"npx playwright test tests/e2e/teach.spec.ts": {Output: "> tsc -b && vite build\nsrc/teach.ts(3,1): error TS2304: Cannot find name 'x'.\n"},
		"npx playwright test tests/e2e/unlock-room.spec.ts": {Output: "Running 1 test using 1 worker\n" +
			"  1) [shots] › tests/e2e/unlock-room.spec.ts:7:1 › Licence page fits ──────\n\n    Error: nope\n\n  1 failed\n"},
	})

	report, err := suite.Run(context.Background(), "lampas")
	if err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	void := map[string]bool{}
	for _, file := range report.Result.Failing {
		void[file.File] = file.Void
		if file.Void != strings.HasPrefix(file.Alone, "recheck void") {
			t.Errorf("%s: void is %v but alone says %q", file.File, file.Void, file.Alone)
		}
	}
	if want := map[string]bool{"tests/e2e/ipa-course.spec.ts": true, "tests/e2e/teach.spec.ts": true, "tests/e2e/unlock-room.spec.ts": false}; !reflect.DeepEqual(void, want) {
		t.Errorf("void by file: got %v, want %v", void, want)
	}

	if stories := tracker.Stories(); len(stories) != 1 {
		t.Errorf("expected one bug filed, for the file that failed again alone, got %v", stories)
	}
	for _, id := range tracker.Stories() {
		story, _ := tracker.ShowStory(context.Background(), id)
		if !strings.Contains(story.Story.Title, "unlock-room.spec.ts") {
			t.Errorf("expected the one bug to be for unlock-room.spec.ts, got %q", story.Story.Title)
		}
	}

	raw, _ := records.File("runs/suite/lampas/2026-10-19.json")
	var kept struct {
		Failing []map[string]any `json:"failing_files"`
	}
	if err := json.Unmarshal([]byte(raw), &kept); err != nil {
		t.Fatalf("reading the summary: %v\n%s", err, raw)
	}
	for _, file := range kept.Failing {
		if want := file["file"] != "tests/e2e/unlock-room.spec.ts"; (file["void"] == true) != want {
			t.Errorf("the summary of %v should say void %v:\n%s", file["file"], want, raw)
		}
	}

	mail, _ := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if len(mail) != 1 || !strings.Contains(mail[0].Body, "recheck void  tests/e2e/ipa-course.spec.ts") || !strings.Contains(mail[0].Body, "is already used") {
		t.Errorf("expected the mail to name the void rechecks and why, got %+v", mail)
	}
	if printed := report.String(); !strings.Contains(printed, "tests/e2e/teach.spec.ts  recheck void") {
		t.Errorf("expected the printed summary to say recheck void, got:\n%s", printed)
	}
}

// TestACappedSuiteLogKeepsItsTail: a phase's output over the cap keeps its first
// part and its last, where Playwright prints its summary and failure blocks.
func TestACappedSuiteLogKeepsItsTail(t *testing.T) {
	long := "Running 580 tests using 4 workers\n" + strings.Repeat("  ✓  1 [shots] › tests/e2e/a.spec.ts:3:1 › fine (1.0s)\n", application.GateLogCap/40) +
		"  1) [shots] › tests/e2e/b.spec.ts:9:1 › the title ──────\n\n    Error: nope\n\n  1 failed\n  579 passed (21.0m)\n"
	suite, records, _, _ := lampasSuite(t, map[string]application.Checked{
		"npm test":             {Passed: true, Output: "ok\n"},
		"npx playwright test ": {Output: long},
		"npx playwright test tests/e2e/b.spec.ts": {Output: "  1) [shots] › tests/e2e/b.spec.ts:9:1 › the title ──────\n\n    Error: nope\n\n  1 failed\n"},
	})
	suite.Filer = nil
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatalf("running the suite: %v", err)
	}
	log, _ := records.File("runs/suite/lampas/2026-10-19.log")
	if len(log) > 2*application.GateLogCap {
		t.Errorf("expected the log to be capped, it is %d bytes", len(log))
	}
	for _, want := range []string{"Running 580 tests using 4 workers", "bytes left out by mw", "Error: nope", "579 passed (21.0m)"} {
		if !strings.Contains(log, want) {
			t.Errorf("expected the capped log to keep %q", want)
		}
	}
}

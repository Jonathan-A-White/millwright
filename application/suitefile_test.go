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

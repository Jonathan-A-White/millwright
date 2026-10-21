package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// timingNight is the nightly timing run of the rig lampas over fakes: a clock
// at 09:00 UTC on 2026-10-21, origin's main at "bbb", the tests the gate leaves
// out in tests/timing/**, and a bug epic to file under.
type timingNight struct {
	night   *application.NightlyTiming
	rig     *apptest.FakeTimingRig
	records *apptest.FakeTimingRecords
	runner  *apptest.FakeTimingRunner
	load    *apptest.FakeHostLoad
	tracker *apptest.FakeTracker
	rules   *apptest.FakeEpicRules
	trees   *apptest.FakeSuiteTrees
	out     *strings.Builder
	home    bool
}

func newTimingNight(t *testing.T) *timingNight {
	t.Helper()
	heads := apptest.NewFakeRigHeads()
	heads.SetHead("/rigs/lampas", "bbb")
	f := &timingNight{
		rig:     &apptest.FakeTimingRig{Tests: map[string][]application.TaggedTest{"/rigs/lampas": {{File: "tests/timing/sw.test.ts", Covers: []string{"src/sw/**"}}}}, Changes: map[string][]string{}},
		records: &apptest.FakeTimingRecords{},
		runner:  &apptest.FakeTimingRunner{},
		load:    &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.2, Cores: 4}},
		tracker: apptest.NewFakeTracker(),
		rules:   apptest.NewFakeEpicRules(),
		trees:   &apptest.FakeSuiteTrees{},
		out:     &strings.Builder{},
		home:    true,
	}
	f.tracker.AddEpic("mw-suite", domain.Path{Rig: "lampas", Branch: "main"})
	f.rules.Require("lampas", domain.EpicRequirements{TimingTests: []string{"tests/timing/**"}, SuiteEpic: "mw-suite"})
	f.night = &application.NightlyTiming{
		Hour: 8, Rigs: map[string]string{"lampas": "/rigs/lampas"}, Tests: map[string]string{"lampas": "npm test"},
		Rules: f.rules, Suites: &apptest.FakeSuiteRecords{}, History: f.records, Records: f.records,
		Source: f.rig, Trees: f.trees, Heads: heads, Runner: f.runner, Uptime: &apptest.FakeUptime{},
		Filer: application.TimingFiler{Tracker: f.tracker, Rules: f.rules},
		Load:  f.load, LoadPoll: time.Second, LoadBound: 3 * time.Second,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Home:     func(context.Context) (bool, error) { return f.home, nil },
		Host:     "desktop",
		Now:      func() time.Time { return time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) },
		Out:      f.out,
	}
	return f
}

// passedAt keeps a passing night of an earlier day at sha.
func (f *timingNight) passedAt(t *testing.T, day, sha string) {
	t.Helper()
	kept, _ := json.Marshal(application.TimingResult{Rig: "lampas", Sha: sha, Passed: true})
	if _, err := f.records.PutTimingFile(context.Background(), "lampas", day+".json", string(kept)); err != nil {
		t.Fatal(err)
	}
}

func (f *timingNight) due(t *testing.T) []string {
	t.Helper()
	due, err := f.night.Due(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var rigs []string
	for _, one := range due {
		rigs = append(rigs, one.Rig)
	}
	return rigs
}

func TestARigWithoutTimingTestsIsNeverDue(t *testing.T) {
	f := newTimingNight(t)
	f.rules.Require("lampas", domain.EpicRequirements{SuiteEpic: "mw-suite"})
	if got := f.due(t); len(got) != 0 {
		t.Errorf("a rig with no timing_tests was due: %v", got)
	}
}

func TestARigWithNoPassingNightIsDue(t *testing.T) {
	f := newTimingNight(t)
	if got := f.due(t); !reflect.DeepEqual(got, []string{"lampas"}) {
		t.Errorf("due = %v, want lampas", got)
	}
}

func TestOnlyANonCoveredFileChangedSinceThePassingNightIsNotDue(t *testing.T) {
	f := newTimingNight(t)
	f.passedAt(t, "2026-10-19", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"README.md", "docs/x.md"}
	if got := f.due(t); len(got) != 0 {
		t.Errorf("due = %v after only README.md changed, want none", got)
	}
	if !strings.Contains(f.out.String(), "nothing its timing tests cover changed since aaa") {
		t.Errorf("the log did not say why, got %q", f.out.String())
	}
	// It was looked at: not again that day.
	f.rig.Changes["aaa..bbb"] = []string{"src/sw/a.ts"}
	if got := f.due(t); len(got) != 0 {
		t.Errorf("a rig found unchanged was looked at again the same day: %v", got)
	}
}

func TestACoveredFileChangedMakesTheRigDue(t *testing.T) {
	f := newTimingNight(t)
	f.passedAt(t, "2026-10-19", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"README.md", "src/sw/backoff.ts"}
	due, err := f.night.Due(context.Background())
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %+v, %v; want lampas", due, err)
	}
	if want := []string{"src/sw/backoff.ts"}; !reflect.DeepEqual(due[0].Covered, want) {
		t.Errorf("covered = %v, want %v", due[0].Covered, want)
	}
	if due[0].Since != "aaa" || due[0].Head != "bbb" {
		t.Errorf("since %q head %q, want aaa and bbb", due[0].Since, due[0].Head)
	}
}

func TestATestFileChangedIsCoveredWhateverItsCoversLineSays(t *testing.T) {
	f := newTimingNight(t)
	f.passedAt(t, "2026-10-19", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"tests/timing/sw.test.ts"}
	if got := f.due(t); len(got) != 1 {
		t.Errorf("due = %v after the test file itself changed, want lampas", got)
	}
}

func TestATestWithNoCoversLineCoversTheWholeRig(t *testing.T) {
	f := newTimingNight(t)
	f.rig.Tests["/rigs/lampas"] = []application.TaggedTest{{File: "tests/timing/sw.test.ts"}}
	f.passedAt(t, "2026-10-19", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"README.md"}
	if got := f.due(t); len(got) != 1 {
		t.Errorf("due = %v after any change, want lampas", got)
	}
}

func TestARigThatWasRunTodayIsNotDue(t *testing.T) {
	f := newTimingNight(t)
	f.passedAt(t, "2026-10-21", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"src/sw/a.ts"}
	if got := f.due(t); len(got) != 0 {
		t.Errorf("a rig with today's result was due: %v", got)
	}
}

func TestARigIsNotDueBeforeTheHourOrBeforeItsSuiteHasRun(t *testing.T) {
	f := newTimingNight(t)
	f.night.Now = func() time.Time { return time.Date(2026, 10, 21, 7, 59, 0, 0, time.UTC) }
	if got := f.due(t); len(got) != 0 {
		t.Errorf("due before the hour: %v", got)
	}

	f = newTimingNight(t)
	f.rules.Require("lampas", domain.EpicRequirements{TimingTests: []string{"tests/timing/**"}, SuiteEpic: "mw-suite", NightlySuite: true})
	if got := f.due(t); len(got) != 0 {
		t.Errorf("due before the rig's suite of today was kept: %v", got)
	}
	suites := &apptest.FakeSuiteRecords{}
	f.night.Suites = suites
	if _, err := suites.PutSuiteFile(context.Background(), "lampas", "2026-10-21.json", `{"rig":"lampas","passed":true}`); err != nil {
		t.Fatal(err)
	}
	if got := f.due(t); len(got) != 1 {
		t.Errorf("not due after the suite of today was kept: %v", got)
	}
}

func TestARigWithTimingTestsAndNoSuiteEpicIsSaidAndNotRun(t *testing.T) {
	f := newTimingNight(t)
	f.rules.Require("lampas", domain.EpicRequirements{TimingTests: []string{"tests/timing/**"}})
	if got := f.due(t); len(got) != 0 {
		t.Errorf("a rig with no suite_epic was due: %v", got)
	}
	if !strings.Contains(f.out.String(), "no suite_epic") {
		t.Errorf("the log did not say the rig has no suite_epic, got %q", f.out.String())
	}
}

func TestAHostThatIsNotTheHomeNeverRunsIt(t *testing.T) {
	f := newTimingNight(t)
	f.home = false
	err := f.night.Job().Run(context.Background())
	var skipped *application.SkippedPassError
	if !errors.As(err, &skipped) {
		t.Fatalf("run = %v, want a skipped pass", err)
	}
	if got := f.runner.Ran(); len(got) != 0 {
		t.Errorf("the tests were run on a host that is not the home: %+v", got)
	}
	if got := len(f.trees.Added()); got != 0 {
		t.Errorf("a worktree was cut on a host that is not the home")
	}
	if _, probed := f.night.Job().Probe(context.Background()); probed {
		t.Errorf("the job sprang on a host that is not the home")
	}
}

func TestAHostThatNeverCalmsSkipsTheNightKeepsNothingAndNamesTheLoad(t *testing.T) {
	f := newTimingNight(t)
	f.load.Reading = application.LoadReading{Load: 6.5, Cores: 4}
	err := f.night.Job().Run(context.Background())
	var skipped *application.SkippedPassError
	if !errors.As(err, &skipped) {
		t.Fatalf("run = %v, want a skipped pass", err)
	}
	if !strings.Contains(skipped.Why, "6.5 of 4 cores") {
		t.Errorf("the event does not name the load: %q", skipped.Why)
	}
	if !strings.Contains(f.out.String(), "6.5 of 4 cores") {
		t.Errorf("the log does not name the load: %q", f.out.String())
	}
	if got := f.runner.Ran(); len(got) != 0 {
		t.Errorf("the tests were run on a loaded host: %+v", got)
	}
	if got := f.records.Paths(); len(got) != 0 {
		t.Errorf("a skipped night kept %v", got)
	}
	if epic, _ := f.tracker.ShowEpic(context.Background(), "mw-suite"); len(epic.Stories) != 0 {
		t.Errorf("a skipped night filed %d bugs", len(epic.Stories))
	}
	// The night is not tried again that day.
	if got := f.due(t); len(got) != 0 {
		t.Errorf("a skipped night was due again the same day: %v", got)
	}
}

func TestTheRunHandsTheRigsCommandTimingOnlyAndItsGlobs(t *testing.T) {
	f := newTimingNight(t)
	f.rules.Require("lampas", domain.EpicRequirements{TimingTests: []string{"tests/timing/**", "**/*.timing.test.ts"}, SuiteEpic: "mw-suite"})
	if err := f.night.Job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	ran := f.runner.Ran()
	if len(ran) != 1 {
		t.Fatalf("ran %d commands, want one: %+v", len(ran), ran)
	}
	if ran[0].Command != "npm test" || ran[0].Rig != "lampas" {
		t.Errorf("ran %q for %q, want npm test for lampas", ran[0].Command, ran[0].Rig)
	}
	want := map[string]string{"MW_TIMING": "only", "MW_TIMING_TESTS": "tests/timing/**,**/*.timing.test.ts"}
	if !reflect.DeepEqual(ran[0].Env, want) {
		t.Errorf("env = %v, want %v", ran[0].Env, want)
	}
	if added := f.trees.Added(); len(added) != 1 || added[0].Commit != "bbb" || !strings.Contains(added[0].Dir, application.TimingTreePrefix) {
		t.Errorf("worktrees = %+v, want one at bbb named timing-", added)
	}
	if f.trees.Live() != 0 {
		t.Errorf("the throwaway worktree was left")
	}
}

func TestAPassingNightKeepsItsCommitAndUptimeAndFilesNothing(t *testing.T) {
	f := newTimingNight(t)
	f.night.Uptime = &apptest.FakeUptime{Lines: []string{"up 3 days, load average: 0.20", "up 3 days, load average: 0.90"}}
	if err := f.night.Job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	kept, ok := f.records.File("runs/timing/lampas/2026-10-21.json")
	if !ok {
		t.Fatalf("no result kept, got %v", f.records.Paths())
	}
	record, err := application.ParseTimingRecord("2026-10-21", kept)
	if err != nil {
		t.Fatal(err)
	}
	r := record.Result
	if !r.Passed || r.Sha != "bbb" || r.UptimeBefore != "up 3 days, load average: 0.20" || r.UptimeAfter != "up 3 days, load average: 0.90" {
		t.Errorf("kept %+v", r)
	}
	if _, ok := f.records.File("runs/timing/lampas/2026-10-21.log"); !ok {
		t.Errorf("no log kept")
	}
	if epic, _ := f.tracker.ShowEpic(context.Background(), "mw-suite"); len(epic.Stories) != 0 {
		t.Errorf("a passing night filed %d bugs", len(epic.Stories))
	}
}

func TestAFailingFileIsFiledOnceAndAnotherNightsFailureIsAComment(t *testing.T) {
	f := newTimingNight(t)
	f.passedAt(t, "2026-10-19", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"src/sw/backoff.ts"}
	f.night.Uptime = &apptest.FakeUptime{Lines: []string{"up 3 days, load average: 0.20"}}
	f.runner.Respond = func(string, string, map[string]string) (application.Checked, error) {
		return application.Checked{Output: " FAIL  tests/timing/sw.test.ts > backs off\nAssertionError: expected 3 to be 2\n"}, nil
	}
	if err := f.night.Job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	epic, err := f.tracker.ShowEpic(context.Background(), "mw-suite")
	if err != nil || len(epic.Stories) != 1 {
		t.Fatalf("stories = %+v, %v; want one bug", epic.Stories, err)
	}
	bug := epic.Stories[0]
	if !strings.Contains(bug.Story.Title, "[bug]") || !strings.Contains(bug.Story.Title, "tests/timing/sw.test.ts") {
		t.Errorf("title %q", bug.Story.Title)
	}
	for _, want := range []string{"bbb", "aaa", "src/sw/backoff.ts", "load average: 0.20"} {
		if !strings.Contains(bug.Description, want) {
			t.Errorf("the bug does not say %q: %s", want, bug.Description)
		}
	}
	if !slicesHas(bug.Labels, application.LabelTiming) || !slicesHas(bug.Labels, application.LabelBug) {
		t.Errorf("labels = %v, want bug and timing", bug.Labels)
	}

	// The next night, with main moved on again, the same file fails again.
	f.night.Now = func() time.Time { return time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC) }
	f.passedAt(t, "2026-10-19", "aaa")
	f.rig.Changes["aaa..bbb"] = []string{"src/sw/backoff.ts"}
	if err := f.night.Job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	epic, _ = f.tracker.ShowEpic(context.Background(), "mw-suite")
	if len(epic.Stories) != 1 {
		t.Errorf("a second failing night filed %d bugs, want still one", len(epic.Stories))
	}
	if got := f.tracker.Comments(bug.Story.ID); len(got) != 1 {
		t.Errorf("comments on the open bug = %v, want one", got)
	}
}

func TestAFailedRunThatNamesNoFileFilesNothingAndFailsThePass(t *testing.T) {
	f := newTimingNight(t)
	f.runner.Respond = func(string, string, map[string]string) (application.Checked, error) {
		return application.Checked{Output: "--- FAIL: TestBackoffTiming (0.31s)\nFAIL\n"}, nil
	}
	if err := f.night.Job().Run(context.Background()); err == nil {
		t.Errorf("a failed run that named no file passed")
	}
	if epic, _ := f.tracker.ShowEpic(context.Background(), "mw-suite"); len(epic.Stories) != 0 {
		t.Errorf("filed %d bugs for a run that named no file", len(epic.Stories))
	}
	kept, ok := f.records.File("runs/timing/lampas/2026-10-21.json")
	if !ok || !strings.Contains(kept, `"passed": false`) {
		t.Errorf("the failed run was not kept as failed: %q", kept)
	}
}

func TestACoversLineIsReadFromTheFirstTenLines(t *testing.T) {
	cases := []struct {
		head string
		want []string
	}{
		{"// covers: src/sw/**, src/retry.ts\nimport x", []string{"src/sw/**", "src/retry.ts"}},
		{"/**\n * covers: src/a.ts src/b/** */\n", []string{"src/a.ts", "src/b/**"}},
		{"# Covers: \"src/x.go\"\n", []string{"src/x.go"}},
		{"// just a timing test\n", nil},
		{strings.Repeat("//\n", 10) + "// covers: src/late.ts\n", nil},
	}
	for _, c := range cases {
		if got := application.ParseCovers(c.head); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseCovers(%q) = %v, want %v", c.head, got, c.want)
		}
	}
}

func slicesHas(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

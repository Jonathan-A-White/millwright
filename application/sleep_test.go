package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// sleepGatherer hands back a fixed bundle and remembers what it was asked.
type sleepGatherer struct {
	bundle application.SleepBundle
	err    error
	asked  []application.SleepWindow
}

func (g *sleepGatherer) Gather(_ context.Context, window application.SleepWindow) (application.SleepBundle, error) {
	g.asked = append(g.asked, window)
	return g.bundle, g.err
}

// sleepVault keeps the plan files, the run files and the commits in memory.
type sleepVault struct {
	mu         sync.Mutex
	principles string
	files      map[string]string // "dir/name" -> contents
	runs       map[string]string
	commits    [][]string
}

func newSleepVault() *sleepVault {
	return &sleepVault{principles: "THE PRINCIPLES", files: map[string]string{}, runs: map[string]string{}}
}

func (v *sleepVault) Principles(context.Context) (string, error) { return v.principles, nil }

func (v *sleepVault) PutPlanFile(_ context.Context, dir, name, contents string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.files[dir+"/"+name] = contents
	return "plans/" + dir + "/" + name, nil
}

func (v *sleepVault) PutRunFile(_ context.Context, kind, name, contents string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.runs[kind+"/"+name] = contents
	return "/vault/runs/" + kind + "/" + name, nil // the real one reports a full path
}

func (v *sleepVault) Commit(_ context.Context, _ string, paths []string) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.commits = append(v.commits, paths)
	return paths, nil
}

func (v *sleepVault) written() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var names []string
	for name := range v.files {
		names = append(names, name)
	}
	for name := range v.runs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fixtureBundle() application.SleepBundle {
	return application.SleepBundle{
		{Name: "triage dump", File: "triage.jsonl", Text: `{"id":"mw-a.1","p":2}` + "\n", Keep: true},
		{Name: "triage diff", File: "diff.txt", Text: "moved: mw-a.1 P3 -> P2\n"},
		{Name: "git log", File: "git-log.txt", Text: "lampas: 41 commits\n"},
		{Name: "runs", File: "runs.txt", Text: "12 sessions\n"},
		{Name: "report", File: "report.json", Text: `{"rigs":[]}` + "\n"},
	}
}

const fixtureAnswer = `{
  "recommendations": "{\"calls\":[{\"bead\":\"mw-a.1\",\"to\":\"P1\",\"why\":\"blocks the arc\"}]}",
  "questions": "# Questions\n1. Keep Lampas polish in Later?\n",
  "maps": "# Maps\nFog lifted: none.\n",
  "improvements": "# Improvements\n## 1. Cut Lampas merge conflicts\nPrediction: tokens -10%.\n",
  "folds": "# Folds\n## lampas: screen:Lampas Review\nBugs: lam-1, lam-2, lam-3. Size: S.\n## lampas: class:stale-cache\nBugs: lam-4, lam-5, lam-6. Shared: refreshCard.\n",
  "summary": "Tonight: one priority call, one question. Lampas merge conflicts are the top refusal cause."
}`

func sleepSettings() application.SleepSettings {
	return application.SleepSettings{
		NightlyModel: "opus", NightlyEffort: "medium",
		SundayModel: "fable", SundayEffort: "xhigh",
	}
}

type sleepFixture struct {
	pass     application.SleepPass
	gatherer *sleepGatherer
	vault    *sleepVault
	grinder  *apptest.FakeGrinder
	mailbox  *apptest.FakeMailbox
	out      *bytes.Buffer
}

// newSleepFixture is a pass run on Friday 2026-10-16 07:30Z.
func newSleepFixture() *sleepFixture {
	f := &sleepFixture{
		gatherer: &sleepGatherer{bundle: fixtureBundle()},
		vault:    newSleepVault(),
		grinder: &apptest.FakeGrinder{Result: application.SessionResult{
			Answer: json.RawMessage(fixtureAnswer),
			Fuel:   application.Fuel{Input: 1000, Output: 2000, CacheRead: 90000, CacheWrite: 7000},
		}},
		mailbox: apptest.NewFakeMailbox(),
		out:     &bytes.Buffer{},
	}
	f.pass = application.SleepPass{
		Gatherer: f.gatherer, Vault: f.vault, Grinder: f.grinder, Mailbox: f.mailbox,
		Settings: sleepSettings(), Instructions: "THE SLEEP INSTRUCTIONS", Host: "laptop",
		Now: func() time.Time { return time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC) },
		Out: f.out,
	}
	return f
}

func (f *sleepFixture) onSunday() {
	f.pass.Now = func() time.Time { return time.Date(2026, 10, 18, 7, 30, 0, 0, time.UTC) }
}

func TestSundayPicksTheSundayModelAndEffort(t *testing.T) {
	settings := sleepSettings()
	for _, tc := range []struct {
		day           time.Time
		model, effort string
		deep          bool
	}{
		{time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC), "opus", "medium", false}, // Friday
		{time.Date(2026, 10, 17, 7, 30, 0, 0, time.UTC), "opus", "medium", false}, // Saturday
		{time.Date(2026, 10, 18, 7, 30, 0, 0, time.UTC), "fable", "xhigh", true},  // Sunday
		{time.Date(2026, 10, 19, 7, 30, 0, 0, time.UTC), "opus", "medium", false}, // Monday
	} {
		model, effort, deep := settings.For(tc.day)
		if model != tc.model || effort != tc.effort || deep != tc.deep {
			t.Errorf("%s: got %s/%s deep=%v, want %s/%s deep=%v", tc.day.Weekday(), model, effort, deep, tc.model, tc.effort, tc.deep)
		}
	}
}

func TestSleepDefaultsAreTheGovernorsCard(t *testing.T) {
	got := application.DefaultSleepSettings()
	if got != sleepSettings() {
		t.Fatalf("defaults = %+v, want opus/medium nightly and fable/xhigh on Sundays", got)
	}
}

func TestAPassOnSundayRunsTheSundayModelOverEverything(t *testing.T) {
	f := newSleepFixture()
	f.onSunday()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := f.grinder.Calls()
	if len(calls) != 1 || calls[0].Call.Model != "fable" || calls[0].Call.Effort != "xhigh" {
		t.Fatalf("calls = %+v, want one grind on fable/xhigh", calls)
	}
	if len(f.gatherer.asked) != 1 || !f.gatherer.asked[0].Deep {
		t.Fatalf("gather asked %+v, want the deep window", f.gatherer.asked)
	}
}

func TestAPassOnAWeekdayRunsTheNightlyModel(t *testing.T) {
	f := newSleepFixture()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := f.grinder.Calls()
	if len(calls) != 1 || calls[0].Call.Model != "opus" || calls[0].Call.Effort != "medium" {
		t.Fatalf("calls = %+v, want one grind on opus/medium", calls)
	}
	if f.gatherer.asked[0].Deep {
		t.Fatal("a weekday pass gathered the deep window")
	}
}

func TestTheFlagsOverrideTheModelAndEffort(t *testing.T) {
	f := newSleepFixture()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{Model: "sonnet", Effort: "low"}); err != nil {
		t.Fatal(err)
	}
	if call := f.grinder.Calls()[0].Call; call.Model != "sonnet" || call.Effort != "low" {
		t.Fatalf("grind = %s/%s, want sonnet/low", call.Model, call.Effort)
	}
}

func TestADryRunPrintsThePromptAndTheBundleSizesAndWritesNothing(t *testing.T) {
	f := newSleepFixture()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	said := f.out.String()
	for _, want := range []string{
		"THE PRINCIPLES", "THE SLEEP INSTRUCTIONS", "opus/medium", "triage.jsonl",
		"triage dump", "triage diff", "git log", "runs", "report", "bytes",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the dry run did not say %q:\n%s", want, said)
		}
	}
	if got := f.vault.written(); len(got) != 0 {
		t.Errorf("a dry run wrote %v", got)
	}
	if len(f.vault.commits) != 0 {
		t.Errorf("a dry run committed %v", f.vault.commits)
	}
	if n := len(f.grinder.Calls()); n != 0 {
		t.Errorf("a dry run started %d agents", n)
	}
	if n := f.mailbox.Writes(); n != 0 {
		t.Errorf("a dry run mailed %d messages", n)
	}
}

func TestADryRunSaysEachPartAgainstItsBudgetAndWhichWereCut(t *testing.T) {
	f := newSleepFixture()
	f.pass.Gatherer = liveSizedGather()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	said := f.out.String()
	for _, want := range []string{"bytes of 50,000", "bytes of 24,000", "bytes of 236,000", "CUT by mw", "cut by mw to fit"} {
		if !strings.Contains(said, want) {
			t.Errorf("the dry run did not say %q:\n%s", want, said[strings.Index(said, "--- bundle"):])
		}
	}
}

func TestARunOnAFixtureBundleWritesTheFilesAndMailsTheSummary(t *testing.T) {
	f := newSleepFixture()
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := "sleep-2026-10-16"
	want := []string{
		dir + "/folds.md", dir + "/improvements.md", dir + "/maps.md", dir + "/questions.md",
		dir + "/recommendations.json", dir + "/summary.md", dir + "/triage.jsonl",
		"sleep/2026-10-16.json",
	}
	if got := f.vault.written(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("written = %v, want %v", got, want)
	}
	if got := f.vault.files[dir+"/maps.md"]; !strings.Contains(got, "Fog lifted") {
		t.Errorf("maps.md = %q", got)
	}
	var calls struct{ Calls []map[string]string }
	if err := json.Unmarshal([]byte(f.vault.files[dir+"/recommendations.json"]), &calls); err != nil || len(calls.Calls) != 1 {
		t.Errorf("recommendations.json = %q (%v)", f.vault.files[dir+"/recommendations.json"], err)
	}
	if got := f.vault.files[dir+"/triage.jsonl"]; !strings.Contains(got, "mw-a.1") {
		t.Errorf("the dump was not kept for tomorrow's diff: %q", got)
	}
	if len(f.vault.commits) != 1 || strings.Join(f.vault.commits[0], ",") != "plans/"+dir+",runs/sleep/2026-10-16.json" {
		t.Fatalf("commits = %v, want one, of the plans directory and the run log by their paths from the vault's root", f.vault.commits)
	}
	if result.Dir != "plans/"+dir || len(result.Files) != 7 {
		t.Errorf("result = %+v", result)
	}

	inbox, err := f.mailbox.Inbox(context.Background(), "mayor")
	if err != nil || len(inbox) != 1 {
		t.Fatalf("the Mayor's inbox = %v (%v), want one summary", inbox, err)
	}
	if !strings.Contains(inbox[0].Body, "Lampas merge conflicts") || !strings.Contains(inbox[0].Subject, "2026-10-16") {
		t.Errorf("summary = %+v", inbox[0])
	}

	// the agent got the principles and the bundle in one fresh grind
	calls1 := f.grinder.Calls()
	if len(calls1) != 1 {
		t.Fatalf("grinds = %d, want one fresh agent", len(calls1))
	}
	call := calls1[0].Call
	if !strings.Contains(call.System, "THE PRINCIPLES") || !strings.Contains(call.System, "THE SLEEP INSTRUCTIONS") {
		t.Errorf("the agent's system prompt lacks the principles or the rule:\n%s", call.System)
	}
	for file, want := range map[string]string{"diff.txt": "mw-a.1 P3 -> P2", "git-log.txt": "41 commits", "runs.txt": "12 sessions", "triage.jsonl": "mw-a.1"} {
		if got := string(calls1[0].Files[file]); !strings.Contains(got, want) {
			t.Errorf("the agent's directory holds %s = %q, want it to hold %q", file, got, want)
		}
	}
	if !strings.Contains(call.Prompt, "diff.txt") {
		t.Errorf("the prompt does not name the files: %s", call.Prompt)
	}
}

func TestTheRunLogCountsTheFuelUnderItsOwnKind(t *testing.T) {
	f := newSleepFixture()
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	var log application.SleepRunLog
	if err := json.Unmarshal([]byte(f.vault.runs["sleep/2026-10-16.json"]), &log); err != nil {
		t.Fatal(err)
	}
	if log.Model != "opus" || log.Effort != "medium" || log.Tokens.Total != 100000 || log.Outcome != application.SleepWritten {
		t.Errorf("log = %+v", log)
	}
}

func TestAFoldsPartIsWrittenAsFoldsMdAndTheSummaryNamesHowManyFolds(t *testing.T) {
	f := newSleepFixture()
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := f.vault.files["sleep-2026-10-16/folds.md"]
	if !strings.Contains(got, "lampas: screen:Lampas Review") || !strings.Contains(got, "refreshCard") {
		t.Errorf("folds.md = %q", got)
	}
	inbox, _ := f.mailbox.Inbox(context.Background(), "mayor")
	if !strings.Contains(inbox[0].Body, "Folds proposed: 2") || !strings.Contains(result.Summary, "Folds proposed: 2") {
		t.Errorf("the summary does not name the two folds: %q", inbox[0].Body)
	}
	if n := len(strings.Fields(inbox[0].Body)); n > 150 {
		t.Errorf("the mail is %d words, want at most 150", n)
	}
}

func TestAnEmptyFoldsPartWritesAFoldsMdThatSaysNone(t *testing.T) {
	for name, answer := range map[string]string{
		"empty":   strings.Replace(fixtureAnswer, `"folds": "# Folds\n## lampas: screen:Lampas Review\nBugs: lam-1, lam-2, lam-3. Size: S.\n## lampas: class:stale-cache\nBugs: lam-4, lam-5, lam-6. Shared: refreshCard.\n"`, `"folds": ""`, 1),
		"missing": strings.Replace(fixtureAnswer, `"folds"`, `"unrelated"`, 1),
	} {
		f := newSleepFixture()
		f.grinder.Result.Answer = json.RawMessage(answer)
		if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := f.vault.files["sleep-2026-10-16/folds.md"]; !strings.Contains(got, "No fold") {
			t.Errorf("%s: folds.md = %q, want it to say none", name, got)
		}
		inbox, _ := f.mailbox.Inbox(context.Background(), "mayor")
		if !strings.Contains(inbox[0].Body, "Folds proposed: 0") {
			t.Errorf("%s: the summary does not say no fold: %q", name, inbox[0].Body)
		}
	}
}

func TestASummaryOver150WordsIsCutToThem(t *testing.T) {
	f := newSleepFixture()
	long := strings.TrimSpace(strings.Repeat("word ", 200))
	answer := strings.Replace(fixtureAnswer, "Tonight: one priority call, one question. Lampas merge conflicts are the top refusal cause.", long, 1)
	f.grinder.Result.Answer = json.RawMessage(answer)
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	inbox, _ := f.mailbox.Inbox(context.Background(), "mayor")
	if n := len(strings.Fields(inbox[0].Body)); n > 150 {
		t.Errorf("the summary is %d words, want at most 150", n)
	}
}

func TestAnAnswerWhoseRecommendationsAreNotJSONWritesNoPlansButKeepsTheFuel(t *testing.T) {
	f := newSleepFixture()
	answer := strings.Replace(fixtureAnswer, `{\"calls\"`, `not json {\"calls\"`, 1)
	f.grinder.Result.Answer = json.RawMessage(answer)
	_, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err == nil || !strings.Contains(err.Error(), "recommendations") {
		t.Fatalf("err = %v, want a refusal naming the recommendations", err)
	}
	for _, name := range f.vault.written() {
		if strings.HasPrefix(name, "sleep-") {
			t.Errorf("a refused answer wrote %s", name)
		}
	}
	var log application.SleepRunLog
	if err := json.Unmarshal([]byte(f.vault.runs["sleep/2026-10-16.json"]), &log); err != nil {
		t.Fatalf("no run log of the spent fuel: %v", err)
	}
	if log.Tokens.Total == 0 || !strings.HasPrefix(log.Outcome, "refused") {
		t.Errorf("log = %+v", log)
	}
	if n := f.mailbox.Writes(); n != 0 {
		t.Errorf("a refused pass mailed %d messages", n)
	}
}

func TestAnAgentThatCouldNotRunWritesNothing(t *testing.T) {
	f := newSleepFixture()
	f.grinder.Err = errors.New("claude: not logged in")
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err == nil {
		t.Fatal("want the agent's failure")
	}
	if got := f.vault.written(); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
}

func TestABundleThatCannotBeGatheredStartsNoAgent(t *testing.T) {
	f := newSleepFixture()
	f.gatherer.err = errors.New("bd is down")
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err == nil {
		t.Fatal("want the gathering failure")
	}
	if n := len(f.grinder.Calls()); n != 0 {
		t.Errorf("%d agents started on no bundle", n)
	}
}

// sleepNotes is the tracker's key-value store.
type sleepNotes struct{ kv map[string]string }

func (n *sleepNotes) Note(_ context.Context, key string) (string, error) { return n.kv[key], nil }
func (n *sleepNotes) SetNote(_ context.Context, key, value string) error {
	n.kv[key] = value
	return nil
}

func TestTheSleepJobIsDueOnceADayFromItsTimeOnTheHomeOnly(t *testing.T) {
	notes := &sleepNotes{kv: map[string]string{}}
	now := time.Date(2026, 10, 16, 7, 29, 0, 0, time.UTC)
	home := true
	runs := 0
	nightly := &application.SleepNightly{
		Hour: 7, Minute: 30, Notes: notes,
		Run:  func(context.Context) error { runs++; return nil },
		Home: func(context.Context) (bool, error) { return home, nil },
		Now:  func() time.Time { return now },
	}
	job := nightly.Job()
	if job.Name != "sleep" {
		t.Fatalf("job name = %q", job.Name)
	}
	ctx := context.Background()
	if _, due := job.Probe(ctx); due {
		t.Fatal("due at 07:29")
	}
	now = time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC)
	home = false
	if _, due := job.Probe(ctx); due {
		t.Fatal("due on a host that is not home")
	}
	home = true
	if _, due := job.Probe(ctx); !due {
		t.Fatal("not due at 07:30 on the home")
	}
	if err := job.Run(ctx); err != nil || runs != 1 {
		t.Fatalf("run: %v, runs = %d", err, runs)
	}
	if _, due := job.Probe(ctx); due {
		t.Fatal("due again the same day")
	}
	now = now.Add(24 * time.Hour)
	if _, due := job.Probe(ctx); !due {
		t.Fatal("not due the next day")
	}
}

func TestAFailedSleepPassIsNotRetriedTheSameDay(t *testing.T) {
	notes := &sleepNotes{kv: map[string]string{}}
	runs := 0
	nightly := &application.SleepNightly{
		Hour: 7, Minute: 30, Notes: notes,
		Run:  func(context.Context) error { runs++; return fmt.Errorf("the model was down") },
		Home: func(context.Context) (bool, error) { return true, nil },
		Now:  func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) },
	}
	job := nightly.Job()
	ctx := context.Background()
	if err := job.Run(ctx); err == nil {
		t.Fatal("want the failure")
	}
	if _, due := job.Probe(ctx); due {
		t.Fatal("a pass that failed is due again the same day")
	}
	if err := madeOrPassedOver(job.Run(ctx)); err != nil || runs != 1 {
		t.Fatalf("a second run the same day: %v, runs = %d", err, runs)
	}
}

// fuelRuns serves run files by kind.
type fuelRuns map[string]map[string]string

func (f fuelRuns) RunFiles(_ context.Context, kind string) ([]string, error) {
	var names []string
	for name := range f[kind] {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (f fuelRuns) ReadRunFile(_ context.Context, kind, name string) (string, error) {
	return f[kind][name], nil
}

func TestFuelShowsTheSleepPassUnderItsOwnKind(t *testing.T) {
	runs := fuelRuns{
		"sleep": {
			"2026-10-15.json": `{"at":"2026-10-15T07:31:00Z","model":"opus","effort":"medium","tokens":{"total":100000}}`,
			"2026-10-11.json": `{"at":"2026-10-11T07:31:00Z","model":"fable","effort":"xhigh","tokens":{"total":500000}}`,
			"2026-09-01.json": `{"at":"2026-09-01T07:31:00Z","model":"opus","effort":"medium","tokens":{"total":99}}`,
		},
		"todo-rank": {
			"2026-10-15.json": `{"at":"2026-10-15T07:00:00Z","model":"sonnet","effort":"medium","tokens":{"total":4000}}`,
		},
	}
	var out bytes.Buffer
	err := application.FuelByKind{Runs: runs, Days: 7, Now: func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	said := out.String()
	for _, want := range []string{"sleep", "2 passes", "600,000 tokens", "opus/medium", "fable/xhigh", "todo-rank", "4,000 tokens"} {
		if !strings.Contains(said, want) {
			t.Errorf("mw fuel did not say %q:\n%s", want, said)
		}
	}
	if strings.Contains(said, "99 tokens") {
		t.Errorf("a pass from outside the window was counted:\n%s", said)
	}
}

func TestEventJobsAreDescribedOneToALine(t *testing.T) {
	jobs := []application.SpringJob{
		application.DispatchJob(time.Hour, nil),
		application.ClockJob("mail-notify", 5*time.Minute, nil),
		{Name: "sleep", Probe: func(context.Context) (string, bool) { return "", false }, About: "daily from 07:30Z"},
	}
	var out bytes.Buffer
	application.WriteEventJobs(&out, jobs)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q, want one a job", lines)
	}
	for i, name := range []string{"dispatch", "mail-notify", "sleep"} {
		if !strings.HasPrefix(lines[i], name) {
			t.Errorf("line %d = %q, want it to start with %s", i, lines[i], name)
		}
	}
	if !strings.Contains(lines[2], "daily from 07:30Z") {
		t.Errorf("the sleep line = %q", lines[2])
	}
	if !strings.Contains(lines[0], "1h0m0s") || !strings.Contains(lines[1], "5m0s") {
		t.Errorf("lines = %q, want each clock named", lines)
	}
}

const fixtureStale = "- mw-5g.6 hands step: its install was overtaken by mw-5g.9 (closed 2026-10-20). Action: retire-step\n" +
	"- mw-lv.1 demo: names v0.1.5, the rig is at 0.1.305. Action: ask-him\n"

// answerWithStale is the fixture answer with a stale.md.
func answerWithStale(t *testing.T) string {
	t.Helper()
	var answer map[string]any
	if err := json.Unmarshal([]byte(fixtureAnswer), &answer); err != nil {
		t.Fatal(err)
	}
	answer["stale"] = fixtureStale
	raw, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAStaleMdInTheAnswerIsWrittenAndTheSummaryNamesHowManyAreStale(t *testing.T) {
	f := newSleepFixture()
	f.grinder.Result.Answer = json.RawMessage(answerWithStale(t))
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dir := "sleep-2026-10-16"
	if got := f.vault.files[dir+"/stale.md"]; got != fixtureStale {
		t.Errorf("stale.md = %q, want the agent's text", got)
	}
	if result.Stale != 2 || !slices.Contains(result.Files, "stale.md") {
		t.Errorf("result = %+v, want 2 stale and stale.md among the files", result)
	}
	if !strings.Contains(result.Summary, "Stale in Needs you: 2") {
		t.Errorf("summary = %q, want it to say 2 are stale", result.Summary)
	}
	inbox, _ := f.mailbox.Inbox(context.Background(), "mayor")
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "Stale in Needs you: 2") || !strings.Contains(inbox[0].Body, "plans/"+dir+"/") {
		t.Errorf("the mail = %+v", inbox)
	}
	if words := len(strings.Fields(inbox[0].Body)); words > 150 {
		t.Errorf("the mail is %d words, want 150 or fewer", words)
	}
}

func TestAnAnswerWithoutAStaleMdStillWritesTheOtherFiles(t *testing.T) {
	f := newSleepFixture()
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.vault.files["sleep-2026-10-16/stale.md"]; ok || slices.Contains(result.Files, "stale.md") {
		t.Errorf("a stale.md was written for an answer with none: %v", result.Files)
	}
	if result.Stale != 0 || !strings.Contains(result.Summary, "Stale in Needs you: none reported") {
		t.Errorf("summary = %q (%d stale)", result.Summary, result.Stale)
	}
	for _, name := range []string{"questions.md", "maps.md", "improvements.md", "summary.md", "recommendations.json"} {
		if f.vault.files["sleep-2026-10-16/"+name] == "" {
			t.Errorf("%s was not written", name)
		}
	}
}

func TestADryRunPrintsTheNeedsYouPartsSizeAmongTheParts(t *testing.T) {
	f := newSleepFixture()
	f.gatherer.bundle = append(fixtureBundle(), application.SleepPart{
		Name: "needs you", File: application.SleepNeedsYouFile, Text: "NEEDS YOU: 3 items\n", Budget: application.SleepBudgetOf(application.SleepNeedsYouFile),
	})
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	said := f.out.String()
	size := fmt.Sprintf("%-20s %d bytes of %s", application.SleepNeedsYouFile, len("NEEDS YOU: 3 items\n"), application.Thousands(application.SleepBudgetOf(application.SleepNeedsYouFile)))
	if !strings.Contains(said, size) {
		t.Errorf("the dry run's sizes lack %q:\n%s", size, said[strings.Index(said, "--- bundle"):])
	}
}

// proposalFixture is a pass whose bundle carries one open demo (d-1) and one
// message of his (direct:aaa), and whose agent answers with answer.
func proposalFixture(t *testing.T, proposals string) (*sleepFixture, *apptest.FakeVerifyBeads) {
	t.Helper()
	f := newSleepFixture()
	beads := &apptest.FakeVerifyBeads{Now: time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC)}
	picture := filepath.Join(t.TempDir(), "direct-bbb.jpg")
	if err := os.WriteFile(picture, []byte("JPEGBYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.gatherer.bundle = append(fixtureBundle(),
		application.SleepPart{
			Name: "evidence", File: application.SleepEvidenceFile, Text: "direct:aaa: I used it\n", Budget: application.SleepBudgetOf(application.SleepEvidenceFile),
			Attach: []application.SleepAttachment{{Name: "direct-bbb.jpg", Path: picture, Size: 9}},
			Verify: &application.SleepVerify{
				Candidates: []application.VerifyCandidate{{Bead: "d-1", Kind: application.VerifyKindDemo, Title: "Demo d-1", Check: "see the green tick"}},
				Evidence: []application.VerifyEvidence{
					{Txid: "direct:aaa", At: time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)},
					{Txid: "direct:bbb", At: time.Date(2026, 10, 16, 5, 30, 0, 0, time.UTC)},
				},
			},
		},
		application.SleepPart{Name: "candidates", File: application.SleepCandidatesFile, Text: "d-1 demo: see the green tick\n", Budget: application.SleepBudgetOf(application.SleepCandidatesFile)},
	)
	answer := fixtureAnswer
	if proposals != "" {
		answer = strings.Replace(fixtureAnswer, `"summary"`, `"proposals": `+proposals+`, "summary"`, 1)
	}
	f.grinder.Result.Answer = json.RawMessage(answer)
	f.pass.Beads = beads
	f.pass.Verify = application.VerifySettings{Confidence: 0.8}
	return f, beads
}

const oneProposal = `[{"bead":"d-1","txid":"direct:aaa","shows":"the green tick after Save","confidence":0.9}]`

func TestAProposalAt09WritesProposalsMdOnePROPOSEDCommentAndIsCountedInTheSummary(t *testing.T) {
	f, beads := proposalFixture(t, oneProposal)
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	call := f.grinder.Calls()[0]
	if string(call.Files["direct-bbb.jpg"]) != "JPEGBYTES" || !strings.Contains(string(call.Files[application.SleepEvidenceFile]), "direct:aaa") ||
		!strings.Contains(string(call.Files[application.SleepCandidatesFile]), "green tick") {
		t.Errorf("the agent's dir lacks the evidence, the picture or the candidates: %v", slices.Collect(func(yield func(string) bool) {
			for name := range call.Files {
				yield(name)
			}
		}))
	}
	if !strings.Contains(call.Call.Schema, `"proposals"`) {
		t.Error("the schema has no proposals")
	}
	if len(beads.Written) != 1 || beads.Written[0].ID != "d-1" ||
		!strings.HasPrefix(beads.Written[0].Text, "PROPOSED VERIFIED by the Governor's use, from direct:aaa (2026-10-16 06:30Z): the green tick after Save (confidence 0.90, opus, run ") {
		t.Fatalf("comments = %+v", beads.Written)
	}
	md := f.vault.files["sleep-2026-10-16/proposals.md"]
	for _, want := range []string{"d-1", "direct:aaa", "the green tick after Save", "0.90", "mw verify confirm d-1 --txid direct:aaa"} {
		if !strings.Contains(md, want) {
			t.Errorf("proposals.md lacks %q:\n%s", want, md)
		}
	}
	if !slices.Contains(result.Files, "proposals.md") || result.Proposed != 1 {
		t.Errorf("result = %+v", result)
	}
	if summary := f.vault.files["sleep-2026-10-16/summary.md"]; !strings.Contains(summary, "Proposed as verified by his use: 1") {
		t.Errorf("summary.md = %q, want the count of proposals", summary)
	}
	inbox, _ := f.mailbox.Inbox(context.Background(), "mayor")
	if len(inbox) != 1 || !strings.Contains(inbox[0].Body, "Proposed as verified by his use: 1") {
		t.Errorf("the mail = %+v", inbox)
	}
	if words := len(strings.Fields(inbox[0].Body)); words > 150 {
		t.Errorf("the mail is %d words, want 150 or fewer", words)
	}
}

func TestAnAnswerWithoutProposalsStillWritesTheOtherFiles(t *testing.T) {
	f, beads := proposalFixture(t, "")
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.vault.files["sleep-2026-10-16/proposals.md"]; ok || len(beads.Written) != 0 || result.Proposed != 0 {
		t.Errorf("a proposal was made from an answer with none: %v, %+v", result.Files, beads.Written)
	}
	for _, name := range []string{"questions.md", "maps.md", "improvements.md", "summary.md", "recommendations.json"} {
		if f.vault.files["sleep-2026-10-16/"+name] == "" {
			t.Errorf("%s was not written", name)
		}
	}
	if strings.Contains(f.vault.files["sleep-2026-10-16/summary.md"], "Proposed as verified") {
		t.Error("summary.md counts proposals when there were none")
	}
}

func TestACandidateAlreadyProposedForThatTxidOrVerifiedGetsNoNewComment(t *testing.T) {
	for name, existing := range map[string]string{
		"proposed": "PROPOSED VERIFIED by the Governor's use, from direct:aaa (2026-10-15 06:30Z): the tick (confidence 0.90, haiku, run 1)",
		"verified": "VERIFIED by the Governor's use, inferred by the Mayor from direct:zzz: the tick (by hand)",
	} {
		t.Run(name, func(t *testing.T) {
			f, beads := proposalFixture(t, oneProposal)
			beads.Comments = map[string][]application.Comment{"d-1": {{Author: "mw", Text: existing}}}
			result, err := f.pass.Run(context.Background(), application.SleepOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(beads.Written) != 0 || result.Proposed != 0 {
				t.Errorf("a comment was written over %s: %+v", name, beads.Written)
			}
			if md := f.vault.files["sleep-2026-10-16/proposals.md"]; !strings.Contains(md, "left out: already") {
				t.Errorf("proposals.md does not say why it was left out:\n%s", md)
			}
		})
	}
}

func TestAProposedDemoIsNotAlsoStale(t *testing.T) {
	f, _ := proposalFixture(t, oneProposal)
	answer := strings.Replace(fixtureAnswer, `"summary"`, `"proposals": `+oneProposal+`, "stale": "- d-1 demo: its epic closed. Action: ask-him\n- mw-x.2 card: moot. Action: clear-card", "summary"`, 1)
	f.grinder.Result.Answer = json.RawMessage(answer)
	result, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stale := f.vault.files["sleep-2026-10-16/stale.md"]
	if strings.Contains(stale, "d-1") || !strings.Contains(stale, "mw-x.2") || result.Stale != 1 {
		t.Errorf("stale.md = %q (%d), want only mw-x.2: a stale demo is a proposal", stale, result.Stale)
	}
}

func TestADryRunPrintsTheEvidenceAndCandidatesSizesAmongTheParts(t *testing.T) {
	f, beads := proposalFixture(t, "")
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	said := f.out.String()
	for _, want := range []string{application.SleepEvidenceFile, application.SleepCandidatesFile, "1 picture"} {
		if !strings.Contains(said, want) {
			t.Errorf("the dry run lacks %q:\n%s", want, said[strings.Index(said, "--- bundle"):])
		}
	}
	if len(beads.Written) != 0 {
		t.Error("a dry run wrote a comment")
	}
}

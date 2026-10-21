package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

type earlierDump struct {
	name, text string
	ok         bool
}

func (e earlierDump) EarlierDump(context.Context, string) (string, string, bool, error) {
	return e.name, e.text, e.ok, nil
}

type sessionsOf []application.SleepSession

func (s sessionsOf) Sessions(context.Context, time.Time) ([]application.SleepSession, error) {
	return s, nil
}

func gatherFixture() application.SleepGather {
	return application.SleepGather{
		Dump: func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, `{"id":"mw-a.1","type":"task","status":"open","p":1}`+"\n")
			return err
		},
		Mw: func(_ context.Context, args ...string) (string, error) {
			return "mw " + strings.Join(args, " ") + "\n", nil
		},
		Git: func(_ context.Context, dir string, days int) (string, error) {
			return "abc123 a commit in " + dir + "\n", nil
		},
		Rigs:    map[string]string{"zeta": "/rigs/zeta", "alpha": "/rigs/alpha"},
		Runs:    sessionsOf{{Story: "mw-a.1", Result: application.SessionResult{ModelID: "claude-sonnet-5-5", Turns: 10, Fuel: application.Fuel{CacheRead: 1000, Output: 50}}}},
		Earlier: earlierDump{name: "plans/sleep-2026-10-15/triage.jsonl", text: `{"id":"mw-a.1","type":"task","status":"open","p":3}` + "\n", ok: true},
		Now:     func() time.Time { return time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC) },
	}
}

func partsByFile(bundle application.SleepBundle) map[string]application.SleepPart {
	parts := map[string]application.SleepPart{}
	for _, part := range bundle {
		parts[part.File] = part
	}
	return parts
}

func TestTheBundleHoldsTheDumpTheDiffTheQueueTheGitLogsTheRunsAndTheReport(t *testing.T) {
	bundle, err := gatherFixture().Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	for file, want := range map[string]string{
		"triage.jsonl":    "mw-a.1",
		"triage-diff.txt": "plans/sleep-2026-10-15/triage.jsonl",
		"queue.json":      "mw queue --json",
		"git-log.txt":     "## alpha\nabc123 a commit in /rigs/alpha",
		"runs.txt":        "claude-sonnet-5-5: 1 sessions",
		"report.txt":      "mw report --since 7d\n",
		"folds.txt":       "no rig has an open or held bug",
		"stuck.txt":       "mw stuck\n",
	} {
		if got := parts[file].Text; !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to hold %q", file, got, want)
		}
	}
	if !parts["triage.jsonl"].Keep {
		t.Error("the dump is not kept for tomorrow's diff")
	}
	if strings.Index(parts["git-log.txt"].Text, "## alpha") > strings.Index(parts["git-log.txt"].Text, "## zeta") {
		t.Error("the rigs are not in name order")
	}
}

func TestTheBundleHoldsTheFoldListOfEachRigWithAnOpenOrHeldBug(t *testing.T) {
	g := gatherFixture()
	g.Dump = func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, strings.Join([]string{
			`{"id":"lam-1","type":"bug","status":"open","rig":"lampas"}`,
			`{"id":"lam-2","type":"bug","status":"open","rig":"lampas"}`,
			`{"id":"lam-3","type":"task","status":"open","labels":["bug"],"rig":"lampas"}`,
			`{"id":"lam-4","type":"bug","status":"deferred","rig":"lampas"}`,
			`{"id":"cai-1","type":"bug","status":"closed","rig":"cairn"}`,
			`{"id":"mw-a.1","type":"task","status":"open","rig":"millwright"}`,
			`{"id":"fel-1","type":"bug","status":"deferred","rig":"fellowship"}`,
		}, "\n")+"\n")
		return err
	}
	var asked []string
	g.Mw = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "fold" {
			asked = append(asked, args[1])
			return args[1] + ": 1 group of 3 or more\n\nscreen:Lampas Review (4)\n", nil
		}
		return "mw " + strings.Join(args, " ") + "\n", nil
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepFoldsFile]
	if !strings.Contains(part.Text, "## lampas\nlampas: 1 group of 3 or more") || !strings.Contains(part.Text, "screen:Lampas Review (4)") {
		t.Errorf("folds = %q, want lampas's mw fold list", part.Text)
	}
	if got := strings.Join(asked, ","); got != "fellowship,lampas" {
		t.Errorf("mw fold was run for %q, want only the rigs with an open or held bug: fellowship,lampas", got)
	}
	if strings.Contains(part.Text, "cairn") || strings.Contains(part.Text, "millwright") {
		t.Errorf("a rig with no open or held bug is in the folds: %q", part.Text)
	}
}

func TestARigWithNoBugAddsNothingToTheFolds(t *testing.T) {
	bundle, err := gatherFixture().Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepFoldsFile]
	if strings.Contains(part.Text, "mw fold") || !strings.Contains(part.Text, "no rig has an open or held bug") {
		t.Errorf("folds = %q, want no fold list and a line that says so", part.Text)
	}
}

func TestAFoldListThatCannotBeReadIsSaidSoAndThePassGoesOn(t *testing.T) {
	g := gatherFixture()
	g.Dump = func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `{"id":"lam-1","type":"bug","status":"open","rig":"lampas"}`+"\n")
		return err
	}
	g.Mw = func(context.Context, ...string) (string, error) { return "", errors.New("bd is busy") }
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	if got := partsByFile(bundle)[application.SleepFoldsFile].Text; !strings.Contains(got, "## lampas\n(this part could not be read: bd is busy)") {
		t.Errorf("folds = %q", got)
	}
}

func TestSundaysBundleReadsThirtyDaysOfReportAndAWeekOfRuns(t *testing.T) {
	bundle, err := gatherFixture().Gather(context.Background(), application.SleepWindow{Date: "2026-10-18", Deep: true})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	if !strings.Contains(parts[application.SleepReportFile].Text, "--since 30d") {
		t.Errorf("report = %q", parts[application.SleepReportFile].Text)
	}
	if !strings.Contains(parts["runs.txt"].Text, "last 7 days") {
		t.Errorf("runs = %q", parts["runs.txt"].Text)
	}
}

func TestAPartThatCannotBeReadIsSaidSoAndThePassGoesOn(t *testing.T) {
	g := gatherFixture()
	g.Mw = func(context.Context, ...string) (string, error) { return "", errors.New("bd is busy") }
	g.Earlier = earlierDump{}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	if !strings.Contains(parts[application.SleepReportFile].Text, "could not be read: bd is busy") {
		t.Errorf("report = %q", parts[application.SleepReportFile].Text)
	}
	if !strings.Contains(parts["triage-diff.txt"].Text, "first pass") {
		t.Errorf("diff = %q", parts["triage-diff.txt"].Text)
	}
}

func TestADumpThatCannotBeMadeEndsTheGathering(t *testing.T) {
	g := gatherFixture()
	g.Dump = func(context.Context, io.Writer) error { return errors.New("no beads database") }
	if _, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"}); err == nil {
		t.Fatal("want the dump's failure")
	}
}

// liveSizedGather is the gather fixture with parts sized like the first live
// pass's bundle (mw-gq6.522): a report of 413 KB, a triage dump of 425 KB and
// a git log of 184 KB.
func liveSizedGather() application.SleepGather {
	g := gatherFixture()
	var dump strings.Builder
	for i := 0; dump.Len() < 425_000; i++ {
		fmt.Fprintf(&dump, `{"id":"mw-a.%d","type":"task","status":"open","priority":2,"labels":["x"],"rig":"alpha","size":"S","parent":"mw-a","created":"2026-10-01T00:00:00Z","updated":"2026-10-15T00:00:00Z","days_since_update":1,"ready":true,"days_ready":1,"deps":[],"dependents":[],"description":"%s","his_priority":false}`+"\n", i, strings.Repeat("d", 500))
	}
	g.Dump = func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, dump.String())
		return err
	}
	g.Mw = func(_ context.Context, args ...string) (string, error) {
		switch args[0] {
		case "report":
			if slices.Contains(args, "--json") {
				return strings.Repeat("r", 413_000), nil
			}
			return strings.Repeat("report line\n", 1500), nil
		case "queue":
			return strings.Repeat("q", 28_500), nil
		}
		return "", nil
	}
	g.Git = func(context.Context, string, int) (string, error) {
		return strings.Repeat("abc1234 2026-10-15 a commit subject that is quite long indeed\n", 1500), nil
	}
	return g
}

func TestABundleSizedLikeTheLiveOneIsCutToWhatTheAgentReadsInAFewTurns(t *testing.T) {
	bundle, err := liveSizedGather().Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	total, budgets := 0, 0
	for _, part := range bundle {
		total += len(part.Text)
		budgets += application.SleepBudgetOf(part.File)
		if part.Budget != application.SleepBudgetOf(part.File) || part.Budget == 0 {
			t.Errorf("%s: the part's budget is %d, the named one %d", part.File, part.Budget, application.SleepBudgetOf(part.File))
		}
		if len(part.Text) > part.Budget {
			t.Errorf("%s is %d bytes, over its budget of %d", part.File, len(part.Text), part.Budget)
		}
	}
	if total >= 200_000 {
		t.Errorf("the bundle is %d bytes, want under 200,000:\n%s", total, bundle.Sizes())
	}
	if budgets != application.SleepTotalBudget {
		t.Errorf("the parts' budgets come to %d, the total budget is %d", budgets, application.SleepTotalBudget)
	}
	if len(bundle) != application.SleepPartCount() {
		t.Errorf("the bundle has %d parts, the budgets name %d", len(bundle), application.SleepPartCount())
	}
}

func TestAPartOverItsBudgetIsCutByMwAndSaysSo(t *testing.T) {
	bundle, err := liveSizedGather().Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	for _, file := range []string{"triage.jsonl", "queue.json", "git-log.txt"} {
		part := parts[file]
		if part.Cut == "" {
			t.Errorf("%s was over its budget and the part does not say it was cut", file)
		}
		if !strings.Contains(part.Text, "mw cut this part") {
			t.Errorf("%s: the text does not say it was cut:\n...%s", file, part.Text[len(part.Text)-300:])
		}
		if !strings.Contains(bundle.Sizes(), part.Cut) {
			t.Errorf("%s: the sizes do not say how it was cut (%q):\n%s", file, part.Cut, bundle.Sizes())
		}
	}
	if parts["runs.txt"].Cut != "" {
		t.Errorf("the runs fit and were cut: %q", parts["runs.txt"].Cut)
	}
}

func TestTheTriageDumpIsCompactedBeforeAnyBeadIsLeftOutAndTheWholeDumpIsKept(t *testing.T) {
	g := gatherFixture()
	var dump strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&dump, `{"id":"mw-a.%d","type":"task","status":"open","priority":2,"created":"2026-10-01T00:00:00Z","updated":"2026-10-15T00:00:00Z","description":"%s"}`+"\n", i, strings.Repeat("d", 500))
	}
	g.Dump = func(_ context.Context, w io.Writer) error { _, err := io.WriteString(w, dump.String()); return err }
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)["triage.jsonl"]
	if got := strings.Count(part.Text, `"id":"mw-a.`); got != 100 {
		t.Errorf("the compacted dump holds %d beads, want all 100:\n%s", got, part.Cut)
	}
	if strings.Contains(part.Text, "created") || strings.Contains(part.Text, strings.Repeat("d", 200)) {
		t.Error("the dump was not compacted")
	}
	if part.Whole != dump.String() {
		t.Error("the whole dump is not kept for tomorrow's diff")
	}
}

func TestACompactedDumpCarriesHisPriorityForABeadLabelledSo(t *testing.T) {
	g := gatherFixture()
	var dump strings.Builder
	for i := 0; i < 100; i++ {
		labels := `["x"]`
		if i == 7 {
			labels = `["x","his-priority"]`
		}
		fmt.Fprintf(&dump, `{"id":"mw-a.%d","type":"task","status":"open","priority":2,"labels":%s,"his_priority":false,"description":"%s"}`+"\n", i, labels, strings.Repeat("d", 500))
	}
	g.Dump = func(_ context.Context, w io.Writer) error { _, err := io.WriteString(w, dump.String()); return err }
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)["triage.jsonl"]
	if got := strings.Count(part.Text, `"his_priority":true`); got != 1 {
		t.Errorf("the compacted dump marks %d beads his_priority, want only mw-a.7:\n%s", got, part.Text[:300])
	}
	for _, line := range strings.Split(part.Text, "\n") {
		if strings.Contains(line, `"his_priority":true`) && !strings.Contains(line, `"id":"mw-a.7"`) {
			t.Errorf("wrong bead marked: %s", line[:80])
		}
	}
}

func TestAMoreBusyRigDoesNotPushTheOthersOutOfTheGitLog(t *testing.T) {
	g := liveSizedGather()
	g.Git = func(_ context.Context, dir string, _ int) (string, error) {
		if dir == "/rigs/alpha" {
			return strings.Repeat("abc1234 2026-10-15 a commit subject that is quite long indeed\n", 1500), nil
		}
		return "def5678 2026-10-15 the one commit\n", nil
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)["git-log.txt"]
	if !strings.Contains(part.Text, "## zeta\ndef5678") {
		t.Errorf("zeta's commit is gone from the log:\n%s", part.Text)
	}
	if !strings.HasPrefix(part.Cut, "alpha ") {
		t.Errorf("the cut is %q, want it to name alpha", part.Cut)
	}
}

func TestAQuietRigsUnusedShareGoesToTheRigsWithMoreToSay(t *testing.T) {
	g := liveSizedGather()
	g.Rigs = map[string]string{}
	sizes := map[string]int{"bsv-kit": 9000, "cairn": 1615, "fellowship": 30000, "tiny": 100, "zed": 0}
	for name := range sizes {
		g.Rigs[name] = "/rigs/" + name
	}
	g.Git = func(_ context.Context, dir string, _ int) (string, error) {
		line := "abc1234 2026-10-15 a commit subject that is quite long indeed\n"
		return strings.Repeat(line, sizes[strings.TrimPrefix(dir, "/rigs/")]/len(line)+1)[:sizes[strings.TrimPrefix(dir, "/rigs/")]/len(line)*len(line)], nil
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)["git-log.txt"]
	if len(part.Text) > part.Budget {
		t.Errorf("the git log is %d bytes, over its budget of %d", len(part.Text), part.Budget)
	}
	// the budget is 20,000: bsv-kit and cairn want less than an equal share of
	// what is left, so they are whole, and the big rig is given the rest.
	if strings.Contains(part.Cut, "bsv-kit") || strings.Contains(part.Cut, "cairn") {
		t.Errorf("a rig under its share was cut: %q", part.Cut)
	}
	for _, rig := range []string{"fellowship"} {
		if !strings.Contains(part.Cut, rig+" was") {
			t.Errorf("the cut %q does not say %s was cut", part.Cut, rig)
		}
	}
	if len(part.Text) < part.Budget-1000 {
		t.Errorf("the git log is %d bytes of %d: the room was left unspent", len(part.Text), part.Budget)
	}
}

func TestEveryPartTheBundleHoldsFitsInATurnEachAndTheAnswerHasTwoMore(t *testing.T) {
	if application.SleepMaxTurns < application.SleepPartCount()+2 {
		t.Errorf("the turn cap is %d, a read of each of %d parts and the answer needs %d",
			application.SleepMaxTurns, application.SleepPartCount(), application.SleepPartCount()+2)
	}
}

func TestTheBundleHoldsWhatMwStuckSaysAndNamesItsRoots(t *testing.T) {
	g := gatherFixture()
	g.Mw = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "stuck" {
			return "stuck: 3 live beads behind 1 held root\nmw-a.9 (iced): 3 behind: mw-a.1 mw-a.2 mw-a.3\n", nil
		}
		return "", nil
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	if got := partsByFile(bundle)[application.SleepStuckFile].Text; !strings.Contains(got, "mw-a.9 (iced): 3 behind") {
		t.Errorf("stuck = %q, want mw stuck's roots", got)
	}

	g.Mw = func(context.Context, ...string) (string, error) { return "", errors.New("bd is busy") }
	bundle, err = g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	if got := partsByFile(bundle)[application.SleepStuckFile].Text; !strings.Contains(got, "bd is busy") {
		t.Errorf("stuck = %q, want it to say it could not be read", got)
	}
}

func TestTheBundleCarriesTheInversionsOfTheDump(t *testing.T) {
	g := gatherFixture()
	g.Dump = func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w,
			`{"id":"mw-a.1","type":"task","status":"open","priority":1,"deps":["mw-a.2"]}`+"\n"+
				`{"id":"mw-a.2","type":"task","status":"open","priority":2,"deps":["mw-a.3"]}`+"\n"+
				`{"id":"mw-a.3","type":"task","status":"open","priority":3,"labels":["his-priority"]}`+"\n")
		return err
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	got := partsByFile(bundle)[application.SleepStuckFile].Text
	for _, want := range []string{"mw-a.2 P2→P1", "mw-a.3 P3 (his)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the bundle's inversions = %q, want %q", got, want)
		}
	}

	g.Dump = func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `{"id":"mw-a.1","type":"task","status":"open","priority":2,"deps":["mw-a.2"]}`+"\n"+
			`{"id":"mw-a.2","type":"task","status":"open","priority":1}`+"\n")
		return err
	}
	bundle, _ = g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if got := partsByFile(bundle)[application.SleepStuckFile].Text; strings.Contains(got, "P2→") || strings.Contains(got, "inversion") && !strings.Contains(got, "no priority inversion") {
		t.Errorf("the bundle names an inversion where there is none: %q", got)
	}
}

// evidenceGather is the fixture with one open demo, a message of his and a
// picture of his in the inbox, written an hour and two hours before the pass.
func evidenceGather(t *testing.T) (application.SleepGather, string) {
	t.Helper()
	g := gatherFixture()
	inbox := t.TempDir()
	now := g.Now()
	for name, age := range map[string]time.Duration{"direct-aaa.txt": time.Hour, "direct-bbb.jpg": 2 * time.Hour, "direct-old.txt": 40 * time.Hour} {
		path := filepath.Join(inbox, name)
		if err := os.WriteFile(path, []byte("I tapped Save and saw "+name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	demo := verifyDemo("d-1", "e-1", "Open the app and look for the green tick.")
	g.Beads = &apptest.FakeVerifyBeads{Work: application.WorkInHand{Ready: []application.StoryDetail{demo}}}
	g.InboxDir = inbox
	return g, inbox
}

func TestTheBundleHoldsTheDaysEvidenceAndTheCandidatesCheckText(t *testing.T) {
	g, inbox := evidenceGather(t)
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	evidence, candidates := parts[application.SleepEvidenceFile], parts[application.SleepCandidatesFile]
	for _, want := range []string{"direct:aaa", "I tapped Save and saw direct-aaa.txt", "direct:bbb", "direct-bbb.jpg"} {
		if !strings.Contains(evidence.Text, want) {
			t.Errorf("evidence.txt lacks %q:\n%s", want, evidence.Text)
		}
	}
	if strings.Contains(evidence.Text, "direct:old") {
		t.Errorf("evidence.txt holds a message from before the window:\n%s", evidence.Text)
	}
	for _, want := range []string{"d-1", "demo", "green tick"} {
		if !strings.Contains(candidates.Text, want) {
			t.Errorf("candidates.txt lacks %q:\n%s", want, candidates.Text)
		}
	}
	for _, part := range []application.SleepPart{evidence, candidates} {
		if part.Budget == 0 || part.Budget != application.SleepBudgetOf(part.File) || len(part.Text) > part.Budget {
			t.Errorf("%s is %d bytes against a budget of %d", part.File, len(part.Text), part.Budget)
		}
	}
	if len(evidence.Attach) != 1 || evidence.Attach[0].Name != "direct-bbb.jpg" || evidence.Attach[0].Path != filepath.Join(inbox, "direct-bbb.jpg") {
		t.Errorf("the picture is not attached by its path: %+v", evidence.Attach)
	}
	if evidence.Verify == nil || len(evidence.Verify.Candidates) != 1 || len(evidence.Verify.Evidence) != 2 {
		t.Errorf("the part does not carry what the proposals are checked against: %+v", evidence.Verify)
	}
	sizes := bundle.Sizes()
	for _, want := range []string{application.SleepEvidenceFile, application.SleepCandidatesFile, "1 picture"} {
		if !strings.Contains(sizes, want) {
			t.Errorf("the sizes lack %q:\n%s", want, sizes)
		}
	}
}

func TestEvidenceAndCandidatesAreCutToTheirBudgets(t *testing.T) {
	g, inbox := evidenceGather(t)
	now := g.Now()
	for n := range 60 {
		path := filepath.Join(inbox, fmt.Sprintf("direct-%03d.txt", n))
		if err := os.WriteFile(path, []byte(strings.Repeat("a long word of his ", 400)), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(path, now.Add(-time.Duration(n)*time.Minute), now.Add(-time.Duration(n)*time.Minute))
	}
	var demos []application.StoryDetail
	for n := range 80 {
		demos = append(demos, verifyDemo(fmt.Sprintf("d-%03d", n), "e-1", strings.Repeat("step one, step two, step three. ", 200)))
	}
	g.Beads = &apptest.FakeVerifyBeads{Work: application.WorkInHand{Ready: demos}}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	parts := partsByFile(bundle)
	if part := parts[application.SleepEvidenceFile]; len(part.Text) > part.Budget || part.Cut == "" {
		t.Errorf("evidence.txt is %d bytes against %d, cut = %q", len(part.Text), part.Budget, part.Cut)
	}
	if part := parts[application.SleepCandidatesFile]; len(part.Text) > part.Budget || !strings.Contains(part.Text, "d-000") {
		t.Errorf("candidates.txt is %d bytes against %d and lacks its first demo", len(part.Text), part.Budget)
	}
}

func TestLandingsThatDoNotFitAreLeftOutOldestFirstAndTheDemosStay(t *testing.T) {
	g, _ := evidenceGather(t)
	now := g.Now()
	fake := &apptest.FakeVerifyBeads{
		Work:     application.WorkInHand{Ready: []application.StoryDetail{verifyDemo("d-1", "e-1", "Open the app and look for the green tick.")}},
		Comments: map[string][]application.Comment{},
	}
	for n := range 300 {
		id := fmt.Sprintf("s-%03d", n)
		fake.Closed = append(fake.Closed, application.ClosedStory{
			ID: id, Title: "Story " + id, Type: "task", EpicID: "e-9", CloseReason: "landed on main", ClosedAt: now.Add(-time.Duration(300-n) * time.Minute),
		})
		fake.Comments[id] = verifyComment(closingWithCheck)
	}
	g.Beads = fake
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepCandidatesFile]
	if len(part.Text) > part.Budget || !strings.Contains(part.Text, "d-1 (demo)") || !strings.Contains(part.Text, "s-299") ||
		strings.Contains(part.Text, "s-000") || !strings.Contains(part.Cut, "older unverified landings left out") {
		t.Errorf("candidates.txt (%d bytes of %d, cut %q) does not keep the demo and the newest landings:\n%.600s", len(part.Text), part.Budget, part.Cut, part.Text)
	}
}

func TestAGatherWithNothingToReadTheEvidenceFromSaysSo(t *testing.T) {
	bundle, err := gatherFixture().Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{application.SleepEvidenceFile, application.SleepCandidatesFile} {
		if text := partsByFile(bundle)[file].Text; !strings.Contains(text, "could not be read") {
			t.Errorf("%s = %q, want it to say it could not be read", file, text)
		}
	}
}

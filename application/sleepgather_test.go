package application_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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

func TestEveryPartTheBundleHoldsFitsInATurnEachAndTheAnswerHasTwoMore(t *testing.T) {
	if application.SleepMaxTurns < application.SleepPartCount()+2 {
		t.Errorf("the turn cap is %d, a read of each of %d parts and the answer needs %d",
			application.SleepMaxTurns, application.SleepPartCount(), application.SleepPartCount()+2)
	}
}

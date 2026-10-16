package application_test

import (
	"context"
	"errors"
	"io"
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
		"report.json":     "mw report --since 7d --json",
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
	if !strings.Contains(parts["report.json"].Text, "--since 30d") {
		t.Errorf("report = %q", parts["report.json"].Text)
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
	if !strings.Contains(parts["report.json"].Text, "could not be read: bd is busy") {
		t.Errorf("report = %q", parts["report.json"].Text)
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

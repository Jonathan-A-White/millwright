package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventsJobsListsSleepWithItsTime(t *testing.T) {
	eventsHome(t)
	defer func(was userUnits) { springUnits = was }(springUnits)
	springUnits = &fakeUnits{installed: map[string]bool{dispatchUnit: true}}

	out, err := runEvents(t, "jobs")
	if err != nil {
		t.Fatalf("mw events jobs: %v\n%s", err, out)
	}
	var sleep string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "sleep ") {
			sleep = line
		}
	}
	if sleep == "" {
		t.Fatalf("mw events jobs does not list sleep:\n%s", out)
	}
	if !strings.Contains(sleep, "07:30Z") {
		t.Errorf("the sleep line = %q, want its default time", sleep)
	}
	for _, other := range []string{"dispatch ", "chain-stamp "} {
		if !strings.Contains(out, other) {
			t.Errorf("mw events jobs lost %q:\n%s", other, out)
		}
	}
}

func TestEventsJobsNamesTheSleepTimeTheConfigSets(t *testing.T) {
	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"laptop\"\n\n[events]\nsleep_hour = \"06:15\"\n")
	t.Setenv("MW_EVENTS_LOG_PATH", filepath.Join(t.TempDir(), "log.jsonl"))
	defer func(was userUnits) { springUnits = was }(springUnits)
	springUnits = &fakeUnits{installed: map[string]bool{}}

	out, err := runEvents(t, "jobs")
	if err != nil || !strings.Contains(out, "from 06:15Z") {
		t.Fatalf("mw events jobs = %q (%v), want sleep from 06:15Z", out, err)
	}
}

func TestTheSleepPassRunsOnTheConfiguredNightlyAndSundayModels(t *testing.T) {
	sunday := time.Date(2026, 10, 18, 7, 30, 0, 0, time.UTC)
	friday := time.Date(2026, 10, 16, 7, 30, 0, 0, time.UTC)

	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"laptop\"\n")
	pass, err := newSleepPass("/nowhere/vault", "laptop", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if m, e, _ := pass.Settings.For(friday); m != "opus" || e != "medium" {
		t.Errorf("a weekday = %s/%s, want opus/medium", m, e)
	}
	if m, e, deep := pass.Settings.For(sunday); m != "fable" || e != "xhigh" || !deep {
		t.Errorf("a Sunday = %s/%s deep=%v, want fable/xhigh", m, e, deep)
	}

	mwConfig(t, "vault = \"/nowhere/vault\"\nhost = \"laptop\"\n\n[sleep]\nnightly_model = sonnet\nnightly_effort = low\nsunday_model = opus\nsunday_effort = high\n")
	pass, err = newSleepPass("/nowhere/vault", "laptop", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if m, e, _ := pass.Settings.For(friday); m != "sonnet" || e != "low" {
		t.Errorf("a weekday = %s/%s, want sonnet/low", m, e)
	}
	if m, e, _ := pass.Settings.For(sunday); m != "opus" || e != "high" {
		t.Errorf("a Sunday = %s/%s, want opus/high", m, e)
	}
}

func TestFuelShowsTheSleepPassUnderItsOwnKind(t *testing.T) {
	vault := t.TempDir()
	mwConfig(t, "vault = \""+vault+"\"\nhost = \"laptop\"\n")
	day := time.Now().UTC().Format(time.DateOnly)
	if err := os.MkdirAll(filepath.Join(vault, "runs", "sleep"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := `{"at":"` + day + `T07:31:00Z","model":"opus","effort":"medium","tokens":{"total":98000}}`
	if err := os.WriteFile(filepath.Join(vault, "runs", "sleep", day+".json"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"fuel"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mw fuel: %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), "sleep") || !strings.Contains(out.String(), "98,000 tokens") || !strings.Contains(out.String(), "opus/medium 1") {
		t.Fatalf("mw fuel = %q, want the sleep pass under its own kind", out)
	}
}

func TestRigGitLogIsACommitALineAndLeavesMergesOut(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "first change")
	git("commit", "-q", "--allow-empty", "-m", "second change")
	got, err := rigGitLog(context.Background(), dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "second change") {
		t.Fatalf("log = %q, want the two commits newest first", got)
	}
	if _, err := rigGitLog(context.Background(), t.TempDir(), 10); err == nil {
		t.Error("a directory that is no repository gave a log")
	}
}

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/giveway"
)

// aStandingMarker puts a fresh give-way marker in a temp state directory and
// points mw at it, with no config file at all: a gated command that gets as far
// as its config would fail, not give way.
func aStandingMarker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MW_GIVEWAY_DIR", dir)
	t.Setenv("MW_HOST", "desktop")
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	if err := (giveway.Store{Dir: dir}).Write(context.Background(), application.GiveWayMarker{Since: now, Seen: now, By: "mw@desktop"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runGiveWay(args ...string) (string, error) {
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

// mw-2891blf.1: every entry that starts heavy work says "gives way since" and
// does nothing while the marker stands, whatever its arguments.
func TestEveryGatedEntryDoesNothingWhileTheHostGivesWay(t *testing.T) {
	aStandingMarker(t)
	for _, path := range giveWayGated {
		out, err := runGiveWay(strings.Fields(path)...)
		if err != nil || !strings.Contains(out, "mw "+path+": gives way since ") {
			t.Errorf("mw %s with a marker standing: err=%v out=%q, want it to say it gives way and do nothing", path, err, out)
		}
	}
}

func TestAGatedEntryRunsAsBeforeWhenNoMarkerStands(t *testing.T) {
	t.Setenv("MW_GIVEWAY_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MW_VAULT", "")
	for _, path := range giveWayGated {
		out, _ := runGiveWay(strings.Fields(path)...)
		if strings.Contains(out, "gives way since") {
			t.Errorf("mw %s said it gives way with no marker: %q", path, out)
		}
	}
}

func TestEveryGatedPathIsACommandMwHas(t *testing.T) {
	// gateByGiveWay panics on a path that names no command: building the tree is the test.
	root := newRootCmd()
	for _, path := range giveWayGated {
		if cmd, _, err := root.Find(strings.Fields(path)); err != nil || cmd.CommandPath() != "mw "+path {
			t.Errorf("%q: %v", path, err)
		}
	}
}

func TestTheFollowersHeavyJobsAreHeldWhileTheHostGivesWayAndTheOthersAreNot(t *testing.T) {
	aStandingMarker(t)
	ran := map[string]bool{}
	var jobs []application.SpringJob
	names := []string{application.NightlySuiteJobName, application.NightlyTimingJobName, application.GristJobName,
		application.GristLevelJobName, application.BatchLandJobName, application.SleepJobName, "dispatch", "mail-notify"}
	for _, name := range names {
		jobs = append(jobs, application.SpringJob{Name: name, Run: func(context.Context) error { ran[name] = true; return nil }})
	}
	var out bytes.Buffer
	gateJobsByGiveWay(jobs, &out)
	for _, job := range jobs {
		if err := job.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		held := giveWayGatedJobs[name]
		if ran[name] == held {
			t.Errorf("job %s: ran=%v but held=%v", name, ran[name], held)
		}
	}
	if strings.Count(out.String(), "gives way since") != len(giveWayGatedJobs) {
		t.Errorf("said %q", out.String())
	}
}

func TestEveryDispatchMwBuildsGivesWay(t *testing.T) {
	t.Setenv("MW_GIVEWAY_DIR", t.TempDir())
	got := withHostRechecks(application.Dispatch{Host: "desktop"}, application.Suite{})
	if got.GiveWay == nil {
		t.Fatal("a dispatch built through withHostRechecks has no give-way gate")
	}
}

func TestGiveWayMarkAndStatusThroughTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MW_GIVEWAY_DIR", dir)
	t.Setenv("MW_HOST", "desktop")
	if out, err := runGiveWay("giveway", "status"); err != nil || !strings.Contains(out, "not giving way") {
		t.Fatalf("status: %v %q", err, out)
	}
	if out, err := runGiveWay("giveway", "mark", "--note", "windows_free_mb=1234", "--note", "who=grace"); err != nil {
		t.Fatalf("mark: %v %q", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "giveway.log"))
	if err != nil || !strings.Contains(string(raw), `"event":"mark"`) || !strings.Contains(string(raw), `"windows_free_mb":"1234"`) {
		t.Fatalf("log = %q, %v", raw, err)
	}
	out, err := runGiveWay("giveway", "status")
	if err != nil || !strings.Contains(out, `"event":"mark"`) {
		t.Fatalf("status after mark: %v %q", err, out)
	}
	if _, err := runGiveWay("giveway", "mark", "--note", "nothing"); err == nil {
		t.Fatal("a --note without = was taken")
	}
	if _, err := runGiveWay("giveway", "dance"); err == nil {
		t.Fatal("an unknown verb was taken")
	}
}

func TestGiveWayHelpStatesTheVerbsTheFlagsAndTheHomesRefusal(t *testing.T) {
	out, err := runGiveWay("giveway", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mw giveway on", "mw giveway off", "mw giveway status", "mw giveway mark", "--session", "--note", "never gives way", "REFUSED"} {
		if !strings.Contains(out, want) {
			t.Errorf("help does not say %q", want)
		}
	}
}

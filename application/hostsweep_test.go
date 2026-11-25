package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// sweepHost is a fake host: the disk (HostSweepFiles) and the tracker
// (HostSweepStories) the sweep reads, and what it removed.
type sweepHost struct {
	trees    []application.HostSweepTree
	landings []application.HostSweepDir
	runs     []application.HostSweepDir
	asides   []application.HostSweepDir
	stories  map[string]application.StoryDetail

	removedTrees []string
	removedDirs  []string
	failOn       string
	showCalls    [][]string
}

func (h *sweepHost) Trees(context.Context) ([]application.HostSweepTree, error) {
	return h.trees, nil
}
func (h *sweepHost) Landings(context.Context) ([]application.HostSweepDir, error) {
	return h.landings, nil
}
func (h *sweepHost) Runs(context.Context) ([]application.HostSweepDir, error)   { return h.runs, nil }
func (h *sweepHost) Asides(context.Context) ([]application.HostSweepDir, error) { return h.asides, nil }

func (h *sweepHost) RemoveTree(_ context.Context, tree application.HostSweepTree) error {
	if tree.Story == h.failOn {
		return errors.New("disk said no")
	}
	h.removedTrees = append(h.removedTrees, tree.Story)
	h.trees = hostSweepWithout(h.trees, func(t application.HostSweepTree) bool { return t.Story == tree.Story })
	return nil
}

func (h *sweepHost) RemoveDir(_ context.Context, path string) error {
	h.removedDirs = append(h.removedDirs, path)
	gone := func(d application.HostSweepDir) bool { return d.Path == path }
	h.landings = hostSweepWithout(h.landings, gone)
	h.runs = hostSweepWithout(h.runs, gone)
	h.asides = hostSweepWithout(h.asides, gone)
	for i := range h.trees {
		h.trees[i].Caches = hostSweepWithout(h.trees[i].Caches, gone)
	}
	return nil
}

func hostSweepWithout[T any](all []T, drop func(T) bool) []T {
	var kept []T
	for _, item := range all {
		if !drop(item) {
			kept = append(kept, item)
		}
	}
	return kept
}

func (h *sweepHost) ShowBeads(_ context.Context, ids []string) ([]application.StoryDetail, error) {
	h.showCalls = append(h.showCalls, ids)
	var found []application.StoryDetail
	for _, id := range ids {
		if detail, ok := h.stories[id]; ok {
			found = append(found, detail)
		}
	}
	return found, nil
}

func hostSweepStory(id, status string, closed time.Time) application.StoryDetail {
	detail := application.StoryDetail{Status: status, ClosedAt: closed}
	detail.Story.ID = id
	return detail
}

var hostSweepNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func hostSweepFixture() *sweepHost {
	days := func(n int) time.Time { return hostSweepNow.Add(-time.Duration(n) * 24 * time.Hour) }
	return &sweepHost{
		trees: []application.HostSweepTree{
			{Story: "mw-done", Rig: "millwright", Dir: "/w/.mw-worktrees/mw-done", Branch: "mw/mw-done", Bytes: 4 << 20},
			{Story: "mw-live", Rig: "millwright", Dir: "/w/.mw-worktrees/mw-live", Branch: "mw/mw-live", Bytes: 1 << 20},
		},
		landings: []application.HostSweepDir{{Path: "/w/.mw-landing-x", Modified: days(2), Bytes: 2 << 20}},
		runs:     []application.HostSweepDir{{Path: "/v/runs/old", Modified: days(20), Bytes: 3 << 20}},
		asides:   []application.HostSweepDir{{Path: "/h/beads-x-aside-1", Modified: days(10), Bytes: 5 << 20}},
		stories: map[string]application.StoryDetail{
			"mw-done": hostSweepStory("mw-done", application.StateClosed, days(3)),
			"mw-live": hostSweepStory("mw-live", "in_progress", time.Time{}),
		},
	}
}

func hostSweeper(host *sweepHost, dry bool, log *apptest.FakeTickLog, out *bytes.Buffer) application.HostSweep {
	sweep := application.HostSweep{
		Files: host, Stories: host, DryRun: dry,
		Now: func() time.Time { return hostSweepNow },
	}
	if log != nil {
		sweep.Log = log
	}
	if out != nil {
		sweep.Out = out
	}
	return sweep
}

func TestHostSweepDryRunListsAndChangesNothing(t *testing.T) {
	host := hostSweepFixture()
	var out bytes.Buffer
	log := &apptest.FakeTickLog{}
	report, err := hostSweeper(host, true, log, &out).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Acts) != 4 {
		t.Fatalf("the dry run listed %d acts, want the four dead ones:\n%s", len(report.Acts), out.String())
	}
	text := out.String()
	for _, want := range []string{
		"/w/.mw-worktrees/mw-done", "4.0 MB", "closed",
		"/w/.mw-landing-x", "2.0 MB", "older than a day",
		"/v/runs/old", "3.0 MB", "20 days",
		"/h/beads-x-aside-1", "5.0 MB", "10 days",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the dry run's list lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "mw-live") {
		t.Errorf("the live story's worktree is listed:\n%s", text)
	}
	if len(host.removedTrees)+len(host.removedDirs) != 0 {
		t.Errorf("a dry run removed %v %v", host.removedTrees, host.removedDirs)
	}
	if lines, _ := log.Read(context.Background()); len(lines) != 0 {
		t.Errorf("a dry run wrote to the log: %v", lines)
	}
}

func TestHostSweepKeepsWhatCannotBeRebuilt(t *testing.T) {
	for _, dry := range []bool{true, false} {
		host := hostSweepFixture()
		host.trees = []application.HostSweepTree{
			{Story: "mw-dirty", Dir: "/w/.mw-worktrees/mw-dirty", Branch: "mw/mw-dirty", Uncommitted: true, Caches: []application.HostSweepDir{{Path: "/w/.mw-worktrees/mw-dirty/node_modules", Bytes: 9 << 20}}},
			{Story: "mw-ahead", Dir: "/w/.mw-worktrees/mw-ahead", Branch: "mw/mw-ahead", Unsaved: "2 commits on mw/mw-ahead are neither on main nor pushed"},
			{Story: "mw-busy", Dir: "/w/.mw-worktrees/mw-busy", Branch: "mw/mw-busy", Busy: "a process is running in it (pid 7)"},
			{Story: "mw-gone", Dir: "/w/.mw-worktrees/mw-gone", Branch: "mw/mw-gone"},
		}
		host.stories = map[string]application.StoryDetail{
			"mw-dirty": hostSweepStory("mw-dirty", application.StateClosed, hostSweepNow.Add(-72*time.Hour)),
			"mw-ahead": hostSweepStory("mw-ahead", application.StateClosed, hostSweepNow.Add(-72*time.Hour)),
			"mw-busy":  hostSweepStory("mw-busy", application.StateClosed, hostSweepNow.Add(-72*time.Hour)),
		}
		host.landings, host.runs, host.asides = nil, nil, nil
		var out bytes.Buffer
		if _, err := hostSweeper(host, dry, &apptest.FakeTickLog{}, &out).Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, id := range host.removedTrees {
			t.Errorf("dry=%v: removed %s, which cannot be rebuilt", dry, id)
		}
		for _, path := range host.removedDirs {
			if strings.Contains(path, "mw-dirty") {
				t.Errorf("dry=%v: removed %s inside a tree with uncommitted changes", dry, path)
			}
		}
		for _, want := range []string{
			"kept: uncommitted changes", "kept: 2 commits on mw/mw-ahead are neither on main nor pushed",
			"kept: a process is running in it", "kept: the tracker has no story mw-gone",
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("dry=%v: the list lacks %q:\n%s", dry, want, out.String())
			}
		}
	}
}

func TestHostSweepRemovesTheDeadAndWritesOneLineEach(t *testing.T) {
	dry := hostSweepFixture()
	var listed bytes.Buffer
	drySweep, err := hostSweeper(dry, true, nil, &listed).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	host := hostSweepFixture()
	log := &apptest.FakeTickLog{}
	report, err := hostSweeper(host, false, log, nil).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Acts) != len(drySweep.Acts) {
		t.Fatalf("the run did %d acts, the dry run listed %d", len(report.Acts), len(drySweep.Acts))
	}
	for i := range report.Acts {
		if report.Acts[i].Path != drySweep.Acts[i].Path {
			t.Errorf("act %d is %s, the dry run listed %s", i, report.Acts[i].Path, drySweep.Acts[i].Path)
		}
	}
	if got := strings.Join(host.removedTrees, ","); got != "mw-done" {
		t.Errorf("removed worktrees %q, want mw-done", got)
	}
	if got := strings.Join(host.removedDirs, ","); got != "/w/.mw-landing-x,/v/runs/old,/h/beads-x-aside-1" {
		t.Errorf("removed directories %q", got)
	}
	lines, _ := log.Read(context.Background())
	if len(lines) != 4 {
		t.Fatalf("the log holds %d lines, want one for each of 4 paths: %v", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, application.TidyLogPrefix+"host: ") {
			t.Errorf("the log line %q does not start as mw tidy's lines do", line)
		}
	}

	again, err := hostSweeper(host, false, log, nil).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Acts) != 0 {
		t.Errorf("a second run listed %v", again.Acts)
	}
	if lines, _ := log.Read(context.Background()); len(lines) != 4 {
		t.Errorf("a second run wrote to the log: %v", lines)
	}
}

func TestHostSweepTakesTheCachesOfAKeptCleanTreeAndNotYoungDirectories(t *testing.T) {
	host := hostSweepFixture()
	host.trees = []application.HostSweepTree{{
		Story: "mw-ahead", Dir: "/w/.mw-worktrees/mw-ahead", Branch: "mw/mw-ahead",
		Unsaved: "1 commit on mw/mw-ahead is neither on main nor pushed",
		Caches:  []application.HostSweepDir{{Path: "/w/.mw-worktrees/mw-ahead/node_modules", Bytes: 9 << 20}},
	}}
	host.stories = map[string]application.StoryDetail{"mw-ahead": hostSweepStory("mw-ahead", application.StateClosed, hostSweepNow.Add(-72*time.Hour))}
	host.landings[0].Modified = hostSweepNow.Add(-2 * time.Hour)
	host.runs[0].Modified = hostSweepNow.Add(-13 * 24 * time.Hour)
	host.asides[0].Modified = hostSweepNow.Add(-6 * 24 * time.Hour)
	if _, err := hostSweeper(host, false, &apptest.FakeTickLog{}, nil).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(host.removedDirs, ","); got != "/w/.mw-worktrees/mw-ahead/node_modules" {
		t.Errorf("removed %q, want only the cache of the kept tree", got)
	}
}

func TestHostSweepKeepsATreeClosedLessThanAnHourAndAFailureIsANote(t *testing.T) {
	host := hostSweepFixture()
	host.stories["mw-done"] = hostSweepStory("mw-done", application.StateClosed, hostSweepNow.Add(-10*time.Minute))
	var out bytes.Buffer
	if _, err := hostSweeper(host, false, &apptest.FakeTickLog{}, &out).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(host.removedTrees) != 0 || !strings.Contains(out.String(), "kept: closed less than an hour ago") {
		t.Errorf("a story closed 10 minutes ago was taken: %v\n%s", host.removedTrees, out.String())
	}

	host = hostSweepFixture()
	host.failOn = "mw-done"
	report, err := hostSweeper(host, false, &apptest.FakeTickLog{}, nil).Run(context.Background())
	if err == nil || len(report.Notes) == 0 {
		t.Errorf("a removal that failed is no error: %v %v", err, report.Notes)
	}
	if len(host.removedDirs) != 3 {
		t.Errorf("the pass stopped at a failure: removed %v", host.removedDirs)
	}
}

func TestHostSweepRunsAreLeftToTheVaultsSync(t *testing.T) {
	host := hostSweepFixture()
	host.runs = append(host.runs, application.HostSweepDir{
		Path: "/v/runs/tracked", Modified: hostSweepNow.Add(-30 * 24 * time.Hour), Bytes: 1 << 20, Tracked: true,
	})
	for _, dry := range []bool{true, false} {
		report, err := hostSweeper(host, dry, nil, nil).Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, act := range report.Acts {
			if act.Path == "/v/runs/tracked" {
				t.Errorf("dry=%v: the sweep means to take a run the vault's git tracks: %+v", dry, act)
			}
		}
		kept := false
		for _, k := range report.Kept {
			if k.Path == "/v/runs/tracked" && strings.Contains(k.Why, "vault") {
				kept = true
			}
		}
		if !kept {
			t.Errorf("dry=%v: the tracked run is not said to be kept for the vault: %+v", dry, report.Kept)
		}
	}
	for _, path := range host.removedDirs {
		if path == "/v/runs/tracked" {
			t.Errorf("a run the vault's git tracks was removed")
		}
	}
	// An untracked run past its age still goes.
	if got := strings.Join(host.removedDirs, ","); !strings.Contains(got, "/v/runs/old") {
		t.Errorf("the untracked old run was not removed: %q", got)
	}
}

// mw-gq6.1730: the sweep asks the tracker for the stories of all its worktrees
// in one call, not one call a worktree.
func TestHostSweepReadsTheStoriesOfThreeWorktreesInOneCall(t *testing.T) {
	host := hostSweepFixture()
	host.trees = append(host.trees, application.HostSweepTree{Story: "mw-third", Rig: "millwright", Dir: "/w/.mw-worktrees/mw-third", Branch: "mw/mw-third"})
	host.stories["mw-third"] = hostSweepStory("mw-third", "in_progress", time.Time{})
	if _, err := hostSweeper(host, true, nil, nil).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(host.showCalls) != 1 {
		t.Fatalf("the sweep made %d reads of the tracker for 3 worktrees, want 1: %v", len(host.showCalls), host.showCalls)
	}
	if got := strings.Join(host.showCalls[0], ","); got != "mw-done,mw-live,mw-third" {
		t.Errorf("the one read asked for %q", got)
	}
}

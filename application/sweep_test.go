package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A Builder's session runs headless (`claude --print ... > result.json.tmp`):
// nothing reaches its pane until it exits, so SweepOutputLines never changes
// across the whole run. These tests cover mw-gq6.100: Sweep must fold a
// session's worktree writes into its sign of life, so a session like that is
// not called stuck just because its pane is blank.
const sweepActivityHost = "vps"

// newSweepActivityFixture is a claimed, running story on the default path,
// with the runner's own Rigs wired so Sweep can place its worktree.
func newSweepActivityFixture(t *testing.T) (*apptest.FakeTracker, *apptest.FakeRunner, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	runner := apptest.NewFakeRunner()

	const id = "mw-act.1"
	tracker.AddEpic("mw-act", domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelSonnet, Effort: domain.EffortHigh, Formula: "tdd-feature",
		Host: sweepActivityHost,
	})
	tracker.AddStory("mw-act", domain.Story{ID: id, Title: id})
	if err := tracker.ClaimStory(context.Background(), id); err != nil {
		t.Fatalf("claiming %s: %v", id, err)
	}
	if err := runner.Start(context.Background(), application.SessionSpec{
		Name:    application.SessionName(id),
		Command: []string{"true"},
	}); err != nil {
		t.Fatalf("starting the session of %s: %v", id, err)
	}
	runner.Write(application.SessionName(id), "still working\n")
	return tracker, runner, id
}

func TestSweepFoldsWorktreeActivityIntoTheFingerprint(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tracker, runner, id := newSweepActivityFixture(t)

	const rigDir = "/rigs/millwright"
	worktree := application.WorktreeDir(rigDir, id)
	activity := map[string]time.Time{}

	sweep := func() application.SweepReport {
		report, err := application.Sweep{
			Tracker:  tracker,
			Runner:   runner,
			Memory:   tracker,
			Host:     sweepActivityHost,
			Rigs:     map[string]string{"millwright": rigDir},
			Activity: func(_ context.Context, dir string) (time.Time, error) { return activity[dir], nil },
			Now:      func() time.Time { return now },
		}.Run(context.Background())
		if err != nil {
			t.Fatalf("sweeping: %v", err)
		}
		return report
	}

	// The worktree gains a file right away, then again after the threshold has
	// passed: the pane never changes, but the worktree does.
	activity[worktree] = now
	sweep()
	now = now.Add(3 * time.Hour)
	activity[worktree] = now
	report := sweep()

	if got := tracker.State(id, application.RunState); got == application.RunStuck {
		t.Fatalf("expected %s not to be recorded stuck once its worktree gained a newer file, got %q", id, got)
	}
	for _, d := range report.Stuck {
		if d.Story.ID == id {
			t.Fatalf("expected the report not to name %s as stuck, got %+v", id, report.Stuck)
		}
	}
}

func TestSweepStillMarksStuckWhenNeitherPaneNorWorktreeChange(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tracker, runner, id := newSweepActivityFixture(t)

	const rigDir = "/rigs/millwright"
	worktree := application.WorktreeDir(rigDir, id)
	// The worktree's newest file never moves — the existing behaviour is
	// unchanged when there really is no sign of life at all.
	fixedNewest := now
	activity := map[string]time.Time{worktree: fixedNewest}

	sweep := func() application.SweepReport {
		report, err := application.Sweep{
			Tracker:  tracker,
			Runner:   runner,
			Memory:   tracker,
			Host:     sweepActivityHost,
			Rigs:     map[string]string{"millwright": rigDir},
			Activity: func(_ context.Context, dir string) (time.Time, error) { return activity[dir], nil },
			Now:      func() time.Time { return now },
		}.Run(context.Background())
		if err != nil {
			t.Fatalf("sweeping: %v", err)
		}
		return report
	}

	sweep()
	now = now.Add(3 * time.Hour)
	report := sweep()

	if got := tracker.State(id, application.RunState); got != application.RunStuck {
		t.Fatalf("expected %s to be recorded %s=%s, got %q", id, application.RunState, application.RunStuck, got)
	}
	found := false
	for _, d := range report.Stuck {
		if d.Story.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the report to name %s as newly stuck, got %+v", id, report.Stuck)
	}
}

func TestSweepLeavesAChangingPaneAloneWithNoWorktreeChange(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tracker, runner, id := newSweepActivityFixture(t)

	const rigDir = "/rigs/millwright"
	worktree := application.WorktreeDir(rigDir, id)
	activity := map[string]time.Time{worktree: now}

	sweep := func() application.SweepReport {
		report, err := application.Sweep{
			Tracker:  tracker,
			Runner:   runner,
			Memory:   tracker,
			Host:     sweepActivityHost,
			Rigs:     map[string]string{"millwright": rigDir},
			Activity: func(_ context.Context, dir string) (time.Time, error) { return activity[dir], nil },
			Now:      func() time.Time { return now },
		}.Run(context.Background())
		if err != nil {
			t.Fatalf("sweeping: %v", err)
		}
		return report
	}

	sweep()
	now = now.Add(3 * time.Hour)
	runner.Write(application.SessionName(id), "still working, printing more\n")
	report := sweep()

	if got := tracker.State(id, application.RunState); got == application.RunStuck {
		t.Fatalf("expected %s not to be recorded stuck while its pane keeps changing, got %q", id, got)
	}
	for _, d := range report.Stuck {
		if d.Story.ID == id {
			t.Fatalf("expected the report not to name %s as stuck, got %+v", id, report.Stuck)
		}
	}
}

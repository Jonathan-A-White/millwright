package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.797: mw-5r3p30.300 sat in progress for 7 days with a dead pane and
// nothing in mw status to say no session was working it. A story in progress
// longer than application.NoSessionNamedAfter with no live session is named, with
// the one command that fixes it.

func noSessionStatus(t *testing.T, tracker *apptest.FakeTracker, runner *apptest.FakeRunner) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		Sessions:    runner,
		Host:        "vps",
		Seat:        "builder",
		HostSilence: 2 * time.Hour,
		Now:         func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

func TestStatusNamesAStoryInProgressPastTheLimitWithNoLiveSession(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	runner := apptest.NewFakeRunner()
	// A refused story with a dead pane, a refused one with no window at all, one
	// whose session is alive, and one claimed only an hour ago.
	for _, id := range []string{"mw-gq6.50", "mw-gq6.51", "mw-gq6.52", "mw-gq6.53"} {
		storyOn(t, tracker, id, "A story", "vps")
	}
	claim(t, tracker, "mw-gq6.50", 7*24*time.Hour)
	claim(t, tracker, "mw-gq6.51", 8*time.Hour)
	claim(t, tracker, "mw-gq6.52", 9*time.Hour)
	claim(t, tracker, "mw-gq6.53", time.Hour)
	for _, id := range []string{"mw-gq6.50", "mw-gq6.51"} {
		mustDo(t, tracker.SetStoryState(context.Background(), id, application.RunState, application.RunBlocked, "(no-findings) refused"))
	}
	for _, id := range []string{"mw-gq6.50", "mw-gq6.52", "mw-gq6.53"} {
		mustDo(t, runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName(id), Command: []string{"claude"}}))
	}
	runner.Exit(application.SessionName("mw-gq6.50"), 1)
	// Alive, whatever its lease says: a heartbeat renews it.
	mustDo(t, tracker.SetLeaseExpires("mw-gq6.52", statusNow.Add(time.Minute)))

	report := noSessionStatus(t, tracker, runner)
	printed := report.String()
	if strings.Count(printed, "NO SESSION ") != 2 {
		t.Fatalf("expected exactly the two stories with no live session named, got:\n%s", printed)
	}
	for _, id := range []string{"mw-gq6.50", "mw-gq6.51"} {
		if !strings.Contains(printed, "mw retry "+id) {
			t.Errorf("expected the command that fixes %s to be named, got:\n%s", id, printed)
		}
	}
	for _, running := range report.Running {
		want := running.Detail.Story.ID == "mw-gq6.50" || running.Detail.Story.ID == "mw-gq6.51"
		if got := !running.NoSession.IsZero(); got != want {
			t.Errorf("%s: no session is %v", running.Detail.Story.ID, got)
		}
	}
}

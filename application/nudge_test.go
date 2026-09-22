package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// nudgeReport runs application.Nudge over the tracker as the VPS, at
// statusNow, with the default limits unless a test overrides them.
func nudgeReport(t *testing.T, tracker *apptest.FakeTracker, nudge application.Nudge) []application.NudgeClause {
	t.Helper()
	nudge.Tracker = tracker
	if nudge.Notes == nil {
		nudge.Notes = tracker
	}
	nudge.Host = "vps"
	if nudge.Now == nil {
		nudge.Now = func() time.Time { return statusNow }
	}
	clauses, err := nudge.Run(context.Background())
	if err != nil {
		t.Fatalf("reading the quiet alarm: %v", err)
	}
	return clauses
}

func claim(t *testing.T, tracker *apptest.FakeTracker, id string, ago time.Duration) {
	t.Helper()
	if err := tracker.ClaimStory(context.Background(), id); err != nil {
		t.Fatalf("claiming %s: %v", id, err)
	}
	if err := tracker.SetStarted(id, statusNow.Add(-ago)); err != nil {
		t.Fatalf("backdating the claim of %s: %v", id, err)
	}
}

func TestNudgeNamesAStoryClaimedPastTheLimitWithNoMailSince(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story that hangs", "vps")
	claim(t, tracker, "mw-gq6.30", 73*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 1 {
		t.Fatalf("expected one clause, got %+v", clauses)
	}
	if clauses[0].Key != "mw-gq6.30" {
		t.Fatalf("expected the clause keyed by the story, got %q", clauses[0].Key)
	}
	if want := "mw-gq6.30 in progress 73 min, no mail"; clauses[0].Text != want {
		t.Fatalf("expected the clause to read %q, got %q", want, clauses[0].Text)
	}
}

func TestNudgeSaysNothingOfAStoryBelowTheLimit(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story well within its time", "vps")
	claim(t, tracker, "mw-gq6.30", 30*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a story below the limit, got %+v", clauses)
	}
}

func TestNudgeSaysNothingOfAStoryWithBlockedOrRefusedMailSinceItWasClaimed(t *testing.T) {
	for _, run := range []string{application.RunBlocked} {
		t.Run(run, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.30", "A story mw next already told the Mayor about", "vps")
			claim(t, tracker, "mw-gq6.30", 90*time.Minute)
			if err := tracker.SetStoryState(context.Background(), "mw-gq6.30", application.RunState, run, "refused"); err != nil {
				t.Fatalf("recording the run state: %v", err)
			}

			clauses := nudgeReport(t, tracker, application.Nudge{})

			if len(clauses) != 0 {
				t.Fatalf("expected no clause once mail (%s) has gone out, got %+v", run, clauses)
			}
		})
	}
}

func TestNudgeSaysNothingOfAStoryThatHasLanded(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story mw next already landed", "vps")
	claim(t, tracker, "mw-gq6.30", 90*time.Minute)
	if err := tracker.CloseStory(context.Background(), "mw-gq6.30", "landed"); err != nil {
		t.Fatalf("closing mw-gq6.30 as landed: %v", err)
	}

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a story that has landed and closed, got %+v", clauses)
	}
}

func TestNudgeRespectsItsOwnConfiguredLimit(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story", "vps")
	claim(t, tracker, "mw-gq6.30", 20*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{NudgeAfter: 10 * time.Minute})

	if len(clauses) != 1 {
		t.Fatalf("expected one clause once the story is past a 10-minute limit, got %+v", clauses)
	}
}

func TestNudgeNamesAHostWhoseLastSyncIsStale(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 31*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 1 {
		t.Fatalf("expected one clause, got %+v", clauses)
	}
	if clauses[0].Key != "host:laptop" {
		t.Fatalf("expected the clause keyed by the host, got %q", clauses[0].Key)
	}
	if want := "laptop last synced 31 min ago"; clauses[0].Text != want {
		t.Fatalf("expected the clause to read %q, got %q", want, clauses[0].Text)
	}
}

func TestNudgeSaysNothingOfAHostSyncedWithinItsLimit(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 10*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a host synced within its limit, got %+v", clauses)
	}
}

func TestNudgeSaysNothingOfAHostThatHasNeverSynced(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Something pathed to the laptop", "laptop")

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a host that has never synced (mw status already says so), got %+v", clauses)
	}
}

func TestNudgeCombinesAStaleStoryAndAStaleHostInOneReport(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story that hangs here", "vps")
	claim(t, tracker, "mw-gq6.30", 73*time.Minute)
	storyOn(t, tracker, "mw-gq6.31", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 31*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 2 {
		t.Fatalf("expected both clauses, got %+v", clauses)
	}
}

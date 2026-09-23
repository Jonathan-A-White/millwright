package application_test

import (
	"context"
	"strings"
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

// mw-gq6.98: a quiet alarm blamed the laptop's stale sync while the VPS's own
// sync was the one actually halted — the VPS could not pull, so the laptop's
// last-sync note had frozen, and the alarm named the wrong host. Once this
// host's own sync is halted, its other-host "last synced" clauses are not to
// be trusted, so they are replaced by one clause naming this host's own halt.
func TestNudgeNamesThisHostsOwnSyncHaltInPlaceOfOtherHostsAges(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.31", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 29*time.Minute)

	marker := apptest.NewFakeSyncHaltMarker()
	at := statusNow.Add(-15 * time.Minute)
	said := "Error: merge conflict — sync halted, nothing pushed."
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: at, Said: said}); err != nil {
		t.Fatalf("writing the marker: %v", err)
	}

	clauses := nudgeReport(t, tracker, application.Nudge{SyncHalt: marker})

	if len(clauses) != 1 {
		t.Fatalf("expected exactly one clause, got %+v", clauses)
	}
	if clauses[0].Key != "sync:vps" {
		t.Fatalf("expected the clause keyed by this host's own halt, got %q", clauses[0].Key)
	}
	if strings.Contains(clauses[0].Text, "last synced") {
		t.Fatalf("expected the halt clause to replace the last-synced clause, got %q", clauses[0].Text)
	}
	if want := at.UTC().Format(application.LastSyncFormat); !strings.Contains(clauses[0].Text, want) {
		t.Fatalf("expected the clause to name the halt time %q, got %q", want, clauses[0].Text)
	}
	if !strings.Contains(clauses[0].Text, said) {
		t.Fatalf("expected the clause to carry what bd said, got %q", clauses[0].Text)
	}
}

// TestNudgeSaysNothingSpecialWhenTheMarkerHoldsNoHalt confirms the old
// last-synced clause still returns once a SyncHalt marker is wired in but
// holds nothing: the mark, not merely the field being set, is what changes
// the report.
func TestNudgeSaysNothingSpecialWhenTheMarkerHoldsNoHalt(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.31", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 29*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{SyncHalt: apptest.NewFakeSyncHaltMarker()})

	if len(clauses) != 1 {
		t.Fatalf("expected the old last-synced clause, got %+v", clauses)
	}
	if clauses[0].Key != "host:laptop" {
		t.Fatalf("expected the clause keyed by the host, got %q", clauses[0].Key)
	}
}

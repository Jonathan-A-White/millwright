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
	if nudge.Host == "" {
		nudge.Host = "vps"
	}
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
	claim(t, tracker, "mw-gq6.30", 5*time.Minute)
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
	claim(t, tracker, "mw-gq6.31", 5*time.Minute)
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
	claim(t, tracker, "mw-gq6.31", 5*time.Minute)
	syncedAt(t, tracker, "laptop", 29*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{SyncHalt: apptest.NewFakeSyncHaltMarker()})

	if len(clauses) != 1 {
		t.Fatalf("expected the old last-synced clause, got %+v", clauses)
	}
	if clauses[0].Key != "host:laptop" {
		t.Fatalf("expected the clause keyed by the host, got %q", clauses[0].Key)
	}
}

// mw-43v9x.3: a host that is not home stays quiet.
func TestNudgeSaysNothingOnAHostThatIsNotHome(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story that hangs", "laptop")
	claim(t, tracker, "mw-gq6.30", 88*time.Minute)
	storyOn(t, tracker, "mw-gq6.31", "Something pathed to the desktop", "desktop")
	claim(t, tracker, "mw-gq6.31", 5*time.Minute)
	syncedAt(t, tracker, "desktop", 90*time.Minute)
	home := &apptest.FakeHomeFile{Text: "desktop 2026-09-29T00:10:00Z mw@desktop"}

	clauses := nudgeReport(t, tracker, application.Nudge{Host: "laptop", Home: home})

	if len(clauses) != 0 {
		t.Fatalf("expected a host that is not home to raise nothing, got %+v", clauses)
	}
}

func TestNudgeSpeaksAsBeforeWhenTheHomeCannotBeTold(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story that hangs", "laptop")
	claim(t, tracker, "mw-gq6.30", 88*time.Minute)

	for name, home := range map[string]*apptest.FakeHomeFile{
		"no file":        {Missing: true},
		"not understood": {Text: "somewhere else entirely"},
	} {
		t.Run(name, func(t *testing.T) {
			clauses := nudgeReport(t, tracker, application.Nudge{Host: "laptop", Home: home})
			if len(clauses) != 1 {
				t.Fatalf("expected the story named as before, got %+v", clauses)
			}
		})
	}
}

func TestNudgeSpeaksOnTheHome(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "A story that hangs", "laptop")
	claim(t, tracker, "mw-gq6.30", 88*time.Minute)
	home := &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}

	clauses := nudgeReport(t, tracker, application.Nudge{Host: "laptop", Home: home})

	if len(clauses) != 1 || clauses[0].Key != "mw-gq6.30" {
		t.Fatalf("expected the long-running story named on the home, got %+v", clauses)
	}
}

// Tonight's desktop alarm fired every hour for a boost that was simply off: no
// claim of its own, so nothing of the home's is waiting on it.
func TestNudgeSaysNothingOfABoostThatHoldsNoClaim(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Ready on the desktop, nobody has it", "desktop")
	syncedAt(t, tracker, "desktop", 90*time.Minute)
	home := &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}

	clauses := nudgeReport(t, tracker, application.Nudge{Host: "laptop", Home: home})

	if len(clauses) != 0 {
		t.Fatalf("expected no sync alarm for a host holding no claim, got %+v", clauses)
	}
}

func TestNudgeNamesABoostThatHoldsAClaimAndHasGoneQuiet(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Claimed on the desktop", "desktop")
	claim(t, tracker, "mw-gq6.30", 10*time.Minute)
	syncedAt(t, tracker, "desktop", 90*time.Minute)
	home := &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}

	clauses := nudgeReport(t, tracker, application.Nudge{Host: "laptop", Home: home})

	if len(clauses) != 1 || clauses[0].Text != "desktop last synced 90 min ago" {
		t.Fatalf("expected the boost named while it holds a claim, got %+v", clauses)
	}
}

// 2026-09-28: bd keeps started_at from the first claim ever, so a story the
// Laptop re-claimed a minute ago alarmed '88 min'. The claim a dispatch
// recorded is the one measured from.
func TestNudgeCountsAStoryFromItsLatestClaim(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Claimed 88 minutes ago, again a minute ago", "vps")
	claim(t, tracker, "mw-gq6.30", 88*time.Minute)
	reclaimed := statusNow.Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.30", map[string]string{application.ClaimedAtField: reclaimed}); err != nil {
		t.Fatalf("recording the latest claim: %v", err)
	}

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 0 {
		t.Fatalf("expected a story claimed a minute ago not to alarm, got %+v", clauses)
	}
}

// aHandsBeadFiledAgo files a bead labelled hitl with no hands step under the
// epic — a need that waits on the Mayor — created ago before statusNow.
func aHandsBeadFiledAgo(t *testing.T, tracker *apptest.FakeTracker, id string, ago time.Duration) {
	t.Helper()
	storyOn(t, tracker, id, "Something only he can do", "vps")
	if err := tracker.SetLabels(id, application.LabelHitl); err != nil {
		t.Fatalf("labelling %s: %v", id, err)
	}
	if err := tracker.SetCreated(id, statusNow.Add(-ago)); err != nil {
		t.Fatalf("dating %s: %v", id, err)
	}
}

func mayorNudge(tracker *apptest.FakeTracker) application.Nudge {
	return application.Nudge{
		Mayor: application.MayorReader{Tracker: tracker, Notes: tracker, Now: func() time.Time { return statusNow }},
	}
}

func TestNudgeNamesAMayorNeedOlderThanThirtyMinutes(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.50", 31*time.Minute)

	clauses := nudgeReport(t, tracker, mayorNudge(tracker))

	if len(clauses) != 1 {
		t.Fatalf("expected one clause, got %+v", clauses)
	}
	if clauses[0].Key != "mayor.mw-gq6.50" {
		t.Fatalf("expected the clause keyed mayor.mw-gq6.50, got %q", clauses[0].Key)
	}
	if !strings.Contains(clauses[0].Text, "mw-gq6.50") || !strings.Contains(clauses[0].Text, "31 min") {
		t.Fatalf("expected the clause to name the bead and its 31 min, got %q", clauses[0].Text)
	}
}

func TestNudgeSaysNothingOfAMayorNeedYoungerThanThirtyMinutes(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.50", 29*time.Minute)

	clauses := nudgeReport(t, tracker, mayorNudge(tracker))

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a mayor need 29 min old, got %+v", clauses)
	}
}

func TestNudgeLeavesTheLandedMemoryAloneWhenItReadsMayorNeeds(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.50", 31*time.Minute)

	nudgeReport(t, tracker, mayorNudge(tracker))

	if note, _ := tracker.Note(context.Background(), application.PosternSnapshotMemoryKey); note != "" {
		t.Fatalf("expected no landed-memory note written, got %q", note)
	}
}

func TestNudgeStillNamesAStaleHostWhenTheMayorNeedsCannotBeRead(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.60", "Held by a host gone quiet", "laptop")
	claim(t, tracker, "mw-gq6.60", 10*time.Minute)
	syncedAt(t, tracker, "laptop", 45*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{Mayor: brokenMayor{}})

	if len(clauses) != 1 || clauses[0].Key != "host:laptop" {
		t.Fatalf("expected the stale host clause alone, got %+v", clauses)
	}
}

type brokenMayor struct{}

func (brokenMayor) MayorNeeds(context.Context, []application.StoryDetail) ([]application.PosternViewNeed, error) {
	return nil, context.DeadlineExceeded
}

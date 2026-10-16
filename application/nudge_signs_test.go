package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.505: a Boost syncs on its dispatch ticks, so its last sync can be
// 24 minutes old while it has just landed and pushed a story. A sync age
// alone is not a sign the host is quiet.
func aBoostHoldingAClaimSyncedLongAgo(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Claimed on cloud2", "cloud2")
	claim(t, tracker, "mw-gq6.30", 90*time.Minute)
	syncedAt(t, tracker, "cloud2", 24*time.Minute)
	return tracker
}

func TestNudgeSaysNothingOfAHostThatPushedAVaultCommitWithinItsLimit(t *testing.T) {
	tracker := aBoostHoldingAClaimSyncedLongAgo(t)
	signs := apptest.NewFakeHostSigns()
	signs.Set("cloud2", application.HostSign{At: statusNow.Add(-3 * time.Minute), What: "vault commit"})

	clauses := nudgeReport(t, tracker, application.Nudge{Signs: signs})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a host that committed to the vault 3 minutes ago, got %+v", clauses)
	}
}

func TestNudgeNamesAHostWhoseNewestSignOfLifeIsPastItsLimitAndSaysWhatItWas(t *testing.T) {
	tracker := aBoostHoldingAClaimSyncedLongAgo(t)
	signs := apptest.NewFakeHostSigns()
	signs.Set("cloud2", application.HostSign{At: statusNow.Add(-22 * time.Minute), What: "vault commit"})

	clauses := nudgeReport(t, tracker, application.Nudge{Signs: signs})

	if len(clauses) != 1 {
		t.Fatalf("expected one clause, got %+v", clauses)
	}
	if want := "cloud2 last synced 24 min ago, newest sign of life a vault commit 22 min ago"; clauses[0].Text != want {
		t.Fatalf("expected the clause to read %q, got %q", want, clauses[0].Text)
	}
}

func TestNudgeNamesAHostThatShowedNoSignOfLifeBesidesItsSync(t *testing.T) {
	tracker := aBoostHoldingAClaimSyncedLongAgo(t)

	clauses := nudgeReport(t, tracker, application.Nudge{Signs: apptest.NewFakeHostSigns()})

	if len(clauses) != 1 || clauses[0].Text != "cloud2 last synced 24 min ago" {
		t.Fatalf("expected the sync age alone when nothing else is seen, got %+v", clauses)
	}
}

func TestNudgeCountsAClaimTheHostRenewedAsASignOfLife(t *testing.T) {
	for _, field := range []string{application.ClaimedAtField, application.SentBackAtField} {
		t.Run(field, func(t *testing.T) {
			tracker := aBoostHoldingAClaimSyncedLongAgo(t)
			at := statusNow.Add(-4 * time.Minute).UTC().Format(time.RFC3339)
			if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.30", map[string]string{field: at}); err != nil {
				t.Fatalf("recording %s: %v", field, err)
			}

			clauses := nudgeReport(t, tracker, application.Nudge{})

			if len(clauses) != 0 {
				t.Fatalf("expected no clause for a host that renewed its claim 4 minutes ago, got %+v", clauses)
			}
		})
	}
}

func TestNudgeStillNamesAHostWhenItsSignsCannotBeRead(t *testing.T) {
	tracker := aBoostHoldingAClaimSyncedLongAgo(t)
	signs := apptest.NewFakeHostSigns()
	signs.Err = errors.New("git log failed")

	clauses := nudgeReport(t, tracker, application.Nudge{Signs: signs})

	if len(clauses) != 1 || !strings.HasPrefix(clauses[0].Text, "cloud2 last synced 24 min ago") {
		t.Fatalf("expected the host named from its sync age when its signs cannot be read, got %+v", clauses)
	}
}

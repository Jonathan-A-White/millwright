package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.523: the quiet alarm reads a host's newest sign of life (mw-gq6.505);
// mw status, the Postern view and the doctor judged a Boost by its sync age
// alone, so a Boost that landed and pushed minutes ago looked silent.

// heartbeatAt records that the lease on a claimed story was last renewed at
// statusNow less ago: the lease runs ClaimLease from a heartbeat.
func heartbeatAt(t *testing.T, tracker *apptest.FakeTracker, id string, ago time.Duration) {
	t.Helper()
	if err := tracker.SetLeaseExpires(id, statusNow.Add(-ago).Add(application.ClaimLease)); err != nil {
		t.Fatalf("renewing the lease of %s: %v", id, err)
	}
}

// aHostSyncedLongAgoHoldingAClaim is a tracker with a story claimed and one
// ready, both pathed to cloud2, whose last sync was 3 hours ago, with its claim
// made 5 hours ago and its lease last renewed just as long ago.
func aHostSyncedLongAgoHoldingAClaim(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "Claimed on cloud2", "cloud2")
	storyOn(t, tracker, "mw-gq6.31", "Ready on cloud2", "cloud2")
	claim(t, tracker, "mw-gq6.30", 5*time.Hour)
	heartbeatAt(t, tracker, "mw-gq6.30", 5*time.Hour)
	syncedAt(t, tracker, "cloud2", 3*time.Hour)
	return tracker
}

func statusWithSigns(t *testing.T, tracker *apptest.FakeTracker, signs application.HostSigns) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		Signs:       signs,
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

func TestStatusDoesNotCallAHostAsleepThatCommittedToTheVaultWithinTheLimit(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	signs := apptest.NewFakeHostSigns()
	signs.Set("cloud2", application.HostSign{At: statusNow.Add(-10 * time.Minute), What: "vault commit"})

	block := section(statusWithSigns(t, tracker, signs))

	if strings.Contains(block, "ASLEEP") || strings.Contains(block, "stranded") || strings.Contains(block, "re-path") {
		t.Fatalf("expected a host that committed 10 minutes ago left awake, got:\n%s", block)
	}
	if want := "newest sign of life a vault commit 10m ago"; !strings.Contains(block, want) {
		t.Fatalf("expected the block to say the newest sign seen, %q, got:\n%s", want, block)
	}
}

func TestStatusDoesNotCallAHostAsleepWhoseClaimedStoryHeartbeatIsWithinTheLimit(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	heartbeatAt(t, tracker, "mw-gq6.30", 1*time.Minute)

	block := section(statusWithSigns(t, tracker, apptest.NewFakeHostSigns()))

	if strings.Contains(block, "ASLEEP") || strings.Contains(block, "stranded") {
		t.Fatalf("expected a host whose claimed story beat a minute ago left awake, got:\n%s", block)
	}
}

func TestStatusStillCallsAHostAsleepWithNoSignWithinTheLimitAndSaysTheNewestSeen(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	signs := apptest.NewFakeHostSigns()
	signs.Set("cloud2", application.HostSign{At: statusNow.Add(-150 * time.Minute), What: "vault commit"})

	block := section(statusWithSigns(t, tracker, signs))

	for _, want := range []string{"cloud2 · ASLEEP, silent 3h00m", "newest sign of life a vault commit 2h30m ago"} {
		if !strings.Contains(block, want) {
			t.Fatalf("expected %q, got:\n%s", want, block)
		}
	}
	if !strings.Contains(block, "stranded") {
		t.Fatalf("expected the work stranded, got:\n%s", block)
	}
}

func TestStatusStillCallsAHostAsleepWhenItsSignsCannotBeRead(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	signs := apptest.NewFakeHostSigns()
	signs.Err = context.DeadlineExceeded

	block := section(statusWithSigns(t, tracker, signs))

	if want := "cloud2 · ASLEEP, silent 3h00m"; !strings.Contains(block, want) {
		t.Fatalf("expected %q, got:\n%s", want, block)
	}
}

func viewAlarms(t *testing.T, tracker *apptest.FakeTracker, signs application.HostSigns) []application.PosternViewNeed {
	t.Helper()
	doc, err := application.PosternView{
		Tracker: tracker,
		Notes:   tracker,
		Signs:   signs,
		Host:    "desktop",
		Now:     func() time.Time { return statusNow },
	}.Build(context.Background())
	if err != nil {
		t.Fatalf("building the view: %v", err)
	}
	var alarms []application.PosternViewNeed
	for _, need := range doc.Needs {
		if need.Kind == application.PosternNeedAlarm {
			alarms = append(alarms, need)
		}
	}
	return alarms
}

func TestPosternViewRaisesNoHostAlarmForAHostThatCommittedToTheVaultWithinTheLimit(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	syncedAt(t, tracker, "cloud2", 45*time.Minute)
	signs := apptest.NewFakeHostSigns()
	signs.Set("cloud2", application.HostSign{At: statusNow.Add(-4 * time.Minute), What: "vault commit"})

	if alarms := viewAlarms(t, tracker, signs); len(alarms) != 0 {
		t.Fatalf("expected no alarm for a host that committed 4 minutes ago, got %+v", alarms)
	}
}

func TestPosternViewRaisesNoHostAlarmForAHostWhoseClaimedStoryHeartbeatIsWithinTheLimit(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	syncedAt(t, tracker, "cloud2", 45*time.Minute)
	heartbeatAt(t, tracker, "mw-gq6.30", 2*time.Minute)

	if alarms := viewAlarms(t, tracker, apptest.NewFakeHostSigns()); len(alarms) != 0 {
		t.Fatalf("expected no alarm for a host whose claimed story beat 2 minutes ago, got %+v", alarms)
	}
}

func TestPosternViewStillRaisesTheHostAlarmWithNoSignWithinTheLimitAndSaysTheNewestSeen(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	syncedAt(t, tracker, "cloud2", 45*time.Minute)
	signs := apptest.NewFakeHostSigns()
	signs.Set("cloud2", application.HostSign{At: statusNow.Add(-30 * time.Minute), What: "vault commit"})

	alarms := viewAlarms(t, tracker, signs)

	if len(alarms) != 1 {
		t.Fatalf("expected one alarm, got %+v", alarms)
	}
	if want := "cloud2 has not synced for 45 min"; alarms[0].Title != want {
		t.Fatalf("expected the title %q, got %q", want, alarms[0].Title)
	}
	if want := "newest sign of life a vault commit 30 min ago"; !strings.Contains(alarms[0].Text, want) {
		t.Fatalf("expected the text to say %q, got %q", want, alarms[0].Text)
	}
}

func TestNudgeCountsAClaimedStoryHeartbeatAsASignOfLife(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	heartbeatAt(t, tracker, "mw-gq6.30", 1*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{Signs: apptest.NewFakeHostSigns()})

	if len(clauses) != 0 {
		t.Fatalf("expected no clause for a host whose claimed story beat a minute ago, got %+v", clauses)
	}
}

func TestNudgeNamesAHostWhoseClaimedStoryHeartbeatIsPastTheLimitAndSaysSo(t *testing.T) {
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	syncedAt(t, tracker, "cloud2", 45*time.Minute)
	heartbeatAt(t, tracker, "mw-gq6.30", 25*time.Minute)

	clauses := nudgeReport(t, tracker, application.Nudge{})

	if len(clauses) != 1 {
		t.Fatalf("expected one clause, got %+v", clauses)
	}
	if want := "cloud2 last synced 45 min ago, newest sign of life a story heartbeat 25 min ago"; clauses[0].Text != want {
		t.Fatalf("expected %q, got %q", want, clauses[0].Text)
	}
}

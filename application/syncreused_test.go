package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// reusedCloud4 is the book of mw-gq6.1145: cloud4 made at madeAgo under the
// name of a box destroyed before it, whose last sync note (syncedAgo) is still
// in the vault under that name.
func reusedCloud4(t *testing.T, madeAgo, syncedAgo time.Duration, making bool) (*apptest.FakeTracker, *apptest.FakeCloudBook) {
	t.Helper()
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.30", "On the box made again", "cloud4")
	claim(t, tracker, "mw-gq6.30", 3*time.Hour)
	syncedAt(t, tracker, "cloud4", syncedAgo)
	up := statusNow.Add(-madeAgo)
	box := application.CloudBox{Name: "cloud4", Up: up, Making: making}
	if !making {
		box.Made = up.Add(time.Minute)
	}
	return tracker, &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
}

// The old cloud4's sync, 22 minutes back, was written before the new cloud4 was
// made 17 minutes back: the quiet alarm does not say cloud4 last synced.
func TestNudgeReadsNoSyncOfABoxMadeAfterItsLastSyncNote(t *testing.T) {
	tracker, book := reusedCloud4(t, 17*time.Minute, 22*time.Minute, false)

	clauses := nudgeReport(t, tracker, application.Nudge{CloudBook: book, CloudPlan: application.CloudPlan{Prefix: "cloud"}})

	for _, c := range clauses {
		if strings.Contains(c.Text, "cloud4") {
			t.Fatalf("expected nothing of the old cloud4's sync, got %+v", clauses)
		}
	}
}

// The control: a sync written after the box was made is the new box's own, and
// the alarm reads it.
func TestNudgeReadsTheSyncOfABoxWrittenAfterItWasMade(t *testing.T) {
	tracker, book := reusedCloud4(t, 60*time.Minute, 41*time.Minute, false)

	clauses := nudgeReport(t, tracker, application.Nudge{CloudBook: book, CloudPlan: application.CloudPlan{Prefix: "cloud"}})

	if len(clauses) != 1 || clauses[0].Text != "cloud4 last synced 41 min ago" {
		t.Fatalf("expected the new box's own sync named, got %+v", clauses)
	}
}

// mw status's OTHER HOSTS reads the same: no last sync of the old box, and the
// box is not called silent for it.
func TestStatusReadsNoSyncOfABoxMadeAfterItsLastSyncNote(t *testing.T) {
	tracker, book := reusedCloud4(t, 17*time.Minute, 5*time.Hour, true)

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		HostSilence: 2 * time.Hour,
		Cloud:       book, CloudPlan: application.CloudPlan{Prefix: "cloud"},
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}

	block := section(report)
	if strings.Contains(block, "last sync") || strings.Contains(block, "silent 5h") {
		t.Errorf("expected nothing of the old box's sync, got:\n%s", block)
	}
	if !strings.Contains(block, "cloud4 · ASLEEP, never synced") {
		t.Errorf("expected cloud4 to read as never synced, got:\n%s", block)
	}
}

// And a sync the new box wrote is read by status as it is.
func TestStatusReadsTheSyncOfABoxWrittenAfterItWasMade(t *testing.T) {
	tracker, book := reusedCloud4(t, 60*time.Minute, 5*time.Minute, false)

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		HostSilence: 2 * time.Hour,
		Cloud:       book, CloudPlan: application.CloudPlan{Prefix: "cloud"},
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if block := section(report); !strings.Contains(block, "cloud4 · synced 5m ago") {
		t.Errorf("expected the new box's sync read, got:\n%s", block)
	}
}

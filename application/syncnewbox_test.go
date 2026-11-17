package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aBoxMadeAnHourAgo is a sync on cloud2 over a book whose cloud2 came up an
// hour before the pinned clock, with the interval of a backup and of a
// collection a day, so that a note three hours old is within it unless it is
// the note of an older box of the name (mw-gq6.1434).
func aBoxMadeAnHourAgo(t *testing.T) (application.Sync, *apptest.FakeTracker) {
	t.Helper()
	sync, _, tracker := keeping(t)
	sync.Host = "cloud2"
	sync.BackupInterval = 24 * time.Hour
	sync.GCInterval = 24 * time.Hour
	sync.CloudPlan = cloudPlan
	sync.CloudBook = &apptest.FakeCloudBook{State: application.CloudState{
		Boxes: []application.CloudBox{{Name: "cloud2", Up: level.Add(-time.Hour)}},
	}}
	return sync, tracker
}

func seedNote(t *testing.T, tracker *apptest.FakeTracker, key string, at time.Time) {
	t.Helper()
	if err := tracker.SetNote(context.Background(), key, at.UTC().Format(application.LastSyncFormat)); err != nil {
		t.Fatalf("seeding %s: %v", key, err)
	}
}

func TestANewBoxBacksUpAtOnceWhateverTheOldBoxOfItsNameLeft(t *testing.T) {
	sync, tracker := aBoxMadeAnHourAgo(t)
	seedNote(t, tracker, application.LastBackupKey("cloud2"), level.Add(-3*time.Hour))

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.BackedUp || tracker.Syncs() != 1 {
		t.Fatalf("expected the new box to back up though the old box's note is inside the interval, got BackedUp=%v, %d cycles", report.BackedUp, tracker.Syncs())
	}
}

func TestANewBoxCollectsAtOnceWhateverTheOldBoxOfItsNameLeft(t *testing.T) {
	sync, tracker := aBoxMadeAnHourAgo(t)
	seedNote(t, tracker, application.LastGCKey("cloud2"), level.Add(-3*time.Hour))

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.GCed || tracker.GCs() != 1 {
		t.Fatalf("expected the new box to collect though the old box's note is inside the interval, got GCed=%v, %d", report.GCed, tracker.GCs())
	}
}

func TestABoxKeepsItsOwnBackupAndCollectionNotesFromAfterItsMake(t *testing.T) {
	sync, tracker := aBoxMadeAnHourAgo(t)
	seedNote(t, tracker, application.LastBackupKey("cloud2"), level.Add(-10*time.Minute))
	seedNote(t, tracker, application.LastGCKey("cloud2"), level.Add(-10*time.Minute))

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if report.BackedUp || report.GCed || tracker.Syncs() != 0 || tracker.GCs() != 0 {
		t.Fatalf("expected no backup or collection inside the interval of the box's own, got %+v", report)
	}
}

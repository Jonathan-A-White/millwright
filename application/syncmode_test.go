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

// keeping is a sync on the host that holds the one beads database, backed up
// every half hour, with the clock pinned.
func keeping(t *testing.T) (application.Sync, *apptest.FakeVaultFiles, *apptest.FakeTracker) {
	t.Helper()
	sync, files, tracker := syncing(t)
	sync.Mode = application.BeadsSyncBackup
	sync.BackupInterval = 30 * time.Minute
	return sync, files, tracker
}

// sharing is a sync on a host whose bd reaches another host's database.
func sharing(t *testing.T) (application.Sync, *apptest.FakeVaultFiles, *apptest.FakeTracker) {
	t.Helper()
	sync, files, tracker := syncing(t)
	sync.Mode = application.BeadsSyncShared
	return sync, files, tracker
}

// backedUpAgo leaves the note of this host's last backup, ago before the pinned
// clock.
func backedUpAgo(t *testing.T, tracker *apptest.FakeTracker, ago time.Duration) {
	t.Helper()
	at := level.Add(-ago).UTC().Format(application.LastSyncFormat)
	if err := tracker.SetNote(context.Background(), application.LastBackupKey("vps"), at); err != nil {
		t.Fatalf("seeding the last backup: %v", err)
	}
}

func note(t *testing.T, tracker *apptest.FakeTracker, key string) string {
	t.Helper()
	said, err := tracker.Note(context.Background(), key)
	if err != nil {
		t.Fatalf("reading %s: %v", key, err)
	}
	return said
}

func TestLastBackupKeyNamesTheHost(t *testing.T) {
	if key := application.LastBackupKey("desktop"); key != "host.desktop.last_backup" {
		t.Fatalf("expected host.desktop.last_backup, got %q", key)
	}
}

func TestAHostThatKeepsTheOneDatabaseRecordsLevelWithoutARemoteCycleBeforeItsBackupIsDue(t *testing.T) {
	sync, files, tracker := keeping(t)
	backedUpAgo(t, tracker, 10*time.Minute)
	sync.Ticks = application.TickLogs{Dispatch: heldLog(t, "2026-09-21T09:00:00Z ok: 1 started")}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got := tracker.Syncs(); got != 0 {
		t.Fatalf("expected no remote cycle ten minutes after a backup, got %d", got)
	}
	if marks, pulls, pushes := files.Moves(); marks+pulls+pushes != 3 {
		t.Fatalf("expected the vault half to run as ever, got %d marks, %d pulls, %d pushes", marks, pulls, pushes)
	}
	if got, want := note(t, tracker, application.LastSyncKey("vps")), level.Format(application.LastSyncFormat); got != want {
		t.Fatalf("expected host.vps.last_sync %q on every sync, got %q", want, got)
	}
	if note(t, tracker, application.TicksKey("vps")) == "" {
		t.Fatal("expected the note of the timers' counts on every sync")
	}
	if !report.At.Equal(level) {
		t.Fatalf("expected the sync level at %s, got %s", level, report.At)
	}
	if report.BackedUp {
		t.Fatal("expected no backup reported")
	}
	if said := report.String(); !strings.Contains(said, "backup not due") || strings.Contains(said, "beads synced") {
		t.Fatalf("expected the report to say no backup was due, and not that beads were synced, got %q", said)
	}
}

func TestAHostThatKeepsTheOneDatabaseBacksItUpOnceTheIntervalHasPassed(t *testing.T) {
	for name, ago := range map[string]time.Duration{"never backed up": -1, "backed up 31 minutes ago": 31 * time.Minute} {
		t.Run(name, func(t *testing.T) {
			sync, _, tracker := keeping(t)
			if ago >= 0 {
				backedUpAgo(t, tracker, ago)
			}

			report, err := sync.Run(context.Background())
			if err != nil {
				t.Fatalf("syncing: %v", err)
			}
			if got := tracker.Syncs(); got != 1 {
				t.Fatalf("expected one remote cycle, got %d", got)
			}
			if got, want := note(t, tracker, application.LastBackupKey("vps")), level.Format(application.LastSyncFormat); got != want {
				t.Fatalf("expected host.vps.last_backup %q, got %q", want, got)
			}
			if !report.BackedUp || !strings.Contains(report.String(), "backed up") {
				t.Fatalf("expected the report to say a backup ran, got %+v: %q", report, report.String())
			}
		})
	}
}

func TestABackupKeepsTheGCCadenceOfAnyOtherSync(t *testing.T) {
	sync, _, tracker := keeping(t)
	backedUpAgo(t, tracker, 10*time.Minute)

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.GCed || tracker.GCs() != 1 {
		t.Fatalf("expected a host that never collected to collect, backup due or not; got GCed=%v, %d", report.GCed, tracker.GCs())
	}
}

func TestABackupThatHaltsIsSaidButDoesNotStopTheHostThatKeepsTheOneDatabase(t *testing.T) {
	sync, _, tracker := keeping(t)
	marker := apptest.NewFakeSyncHaltMarker()
	sync.SyncHalts = marker
	tracker.SyncExits(2, "conflict in the working set")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a halted backup not to stop the sync, got %v", err)
	}
	if got := tracker.Syncs(); got != 2 {
		t.Fatalf("expected the conflict's one retry, got %d cycles", got)
	}
	if got, want := note(t, tracker, application.LastSyncKey("vps")), level.Format(application.LastSyncFormat); got != want {
		t.Fatalf("expected host.vps.last_sync kept at %q: every other host reads this one database, got %q", want, got)
	}
	if got := note(t, tracker, application.LastBackupKey("vps")); got != "" {
		t.Fatalf("expected no backup recorded, got %q", got)
	}
	if report.BackedUp || !strings.Contains(report.BackupHalt, "merge conflict") {
		t.Fatalf("expected the report to carry the halt, got %+v", report)
	}
	if said := report.String(); !strings.Contains(said, "backup halted") {
		t.Fatalf("expected the report to say the backup halted, got %q", said)
	}
	if _, there, _ := marker.Read(context.Background()); !there {
		t.Fatal("expected this host's own mark of the halt")
	}
	if note(t, tracker, application.SyncHaltKey("vps")) == "" {
		t.Fatal("expected the halt noted in the one database too")
	}
	if tracker.GCs() != 0 {
		t.Fatal("expected no collection on a database whose backup has just halted")
	}
}

func TestABackupThatGetsThroughClearsTheMarksOfAnEarlierHalt(t *testing.T) {
	sync, _, tracker := keeping(t)
	marker := apptest.NewFakeSyncHaltMarker()
	sync.SyncHalts = marker
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: level.Add(-time.Hour), Said: "conflict"}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(context.Background(), application.SyncHaltKey("vps"), "earlier"); err != nil {
		t.Fatal(err)
	}

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if _, there, _ := marker.Read(context.Background()); there {
		t.Fatal("expected the mark cleared once a backup got through")
	}
	if got := note(t, tracker, application.SyncHaltKey("vps")); got != "" {
		t.Fatalf("expected the halt note cleared, got %q", got)
	}
}

func TestAHostWhoseBeadsLiveElsewhereNeverCyclesNorCollectsButRecordsLevel(t *testing.T) {
	sync, files, tracker := sharing(t)
	sync.Ticks = application.TickLogs{Dispatch: heldLog(t, "2026-09-21T09:00:00Z ok: 1 started")}
	marker := apptest.NewFakeSyncHaltMarker()
	sync.SyncHalts = marker
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: level.Add(-time.Hour), Said: "left from before"}); err != nil {
		t.Fatal(err)
	}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got := tracker.Syncs(); got != 0 {
		t.Fatalf("expected no remote cycle, got %d", got)
	}
	if got := tracker.GCs(); got != 0 || report.GCed {
		t.Fatalf("expected no collection: the database's own host does that; got %d", got)
	}
	if marks, pulls, pushes := files.Moves(); marks+pulls+pushes != 3 {
		t.Fatalf("expected the vault half to run as ever, got %d marks, %d pulls, %d pushes", marks, pulls, pushes)
	}
	if got, want := note(t, tracker, application.LastSyncKey("vps")), level.Format(application.LastSyncFormat); got != want {
		t.Fatalf("expected host.vps.last_sync %q, got %q", want, got)
	}
	if note(t, tracker, application.TicksKey("vps")) == "" {
		t.Fatal("expected the note of the timers' counts")
	}
	if _, there, _ := marker.Read(context.Background()); there {
		t.Fatal("expected a mark left from before cleared: nothing here can halt")
	}
	if said := report.String(); strings.Contains(said, "beads synced") || !strings.Contains(said, "another host") {
		t.Fatalf("expected the report to say the beads are another host's, got %q", said)
	}
}

func TestABlockedVaultOnTheHostThatKeepsTheOneDatabaseStillBacksUpWhenDue(t *testing.T) {
	sync, files, tracker := keeping(t)
	files.Dirty = []string{"seats/mayor/ledger.md"}

	report, err := sync.Run(context.Background())
	if _, blocked := application.Blocked(err); !blocked {
		t.Fatalf("expected the vault half blocked, got %v", err)
	}
	if got := tracker.Syncs(); got != 1 {
		t.Fatalf("expected the due backup to run anyway, got %d cycles", got)
	}
	if !report.BackedUp {
		t.Fatal("expected the report to say a backup ran")
	}
	if got := note(t, tracker, application.LastSyncKey("vps")); got != "" {
		t.Fatalf("expected nothing recorded under host.vps.last_sync for a host that is not level, got %q", got)
	}
}

func TestABlockedVaultOnAHostWhoseBeadsLiveElsewhereCyclesNothing(t *testing.T) {
	sync, files, tracker := sharing(t)
	files.Dirty = []string{"seats/mayor/ledger.md"}

	_, err := sync.Run(context.Background())
	if _, blocked := application.Blocked(err); !blocked {
		t.Fatalf("expected the vault half blocked, got %v", err)
	}
	if got := tracker.Syncs(); got != 0 {
		t.Fatalf("expected no remote cycle, got %d", got)
	}
}

func TestASyncModeNobodyKnowsIsRefusedNamingTheThree(t *testing.T) {
	sync, _, tracker := syncing(t)
	sync.Mode = "server"

	_, err := sync.Run(context.Background())
	if err == nil {
		t.Fatal("expected an unknown mode to be refused")
	}
	for _, want := range []string{"remote", "backup", "shared"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to name %q, got %q", want, err)
		}
	}
	if tracker.Syncs() != 0 {
		t.Fatal("expected nothing run")
	}
}

func TestParseBeadsSyncMode(t *testing.T) {
	for said, want := range map[string]application.BeadsSyncMode{
		"":       application.BeadsSyncRemote,
		"remote": application.BeadsSyncRemote,
		"backup": application.BeadsSyncBackup,
		"shared": application.BeadsSyncShared,
	} {
		if got, err := application.ParseBeadsSyncMode(said); err != nil || got != want {
			t.Errorf("ParseBeadsSyncMode(%q) = %q, %v; want %q", said, got, err, want)
		}
	}
	if _, err := application.ParseBeadsSyncMode("server"); err == nil {
		t.Error("expected server refused")
	}
}

func TestANoteThatCannotBeWrittenStillFailsAHostThatKeepsTheOneDatabase(t *testing.T) {
	sync, _, tracker := keeping(t)
	tracker.Err = errors.New("the database is locked")

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected a note that cannot be written to be a failure")
	}
}

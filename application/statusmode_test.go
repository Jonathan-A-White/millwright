package application_test

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// modeStatus is otherHostStatus with a beads sync mode, and this host's own
// halt marker when one is given.
func modeStatus(t *testing.T, tracker *apptest.FakeTracker, mode application.BeadsSyncMode, marker application.SyncHaltMarker) application.StatusReport {
	t.Helper()
	report, err := application.Status{
		Tracker:     tracker,
		Notes:       tracker,
		SyncHalt:    marker,
		Host:        "vps",
		Seat:        "builder",
		SyncMode:    mode,
		HostSilence: 2 * time.Hour,
		Now:         func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

func TestStatusSaysHowThisHostsBeadsAreSynced(t *testing.T) {
	for mode, want := range map[application.BeadsSyncMode]string{
		"":                          "BEADS SYNC remote",
		application.BeadsSyncRemote: "BEADS SYNC remote",
		application.BeadsSyncShared: "BEADS SYNC shared · kept on another host",
		application.BeadsSyncBackup: "BEADS SYNC backup · never backed up",
	} {
		printed := modeStatus(t, aTrackerPathedToVPS(t), mode, nil).String()
		if !strings.Contains(printed, want) {
			t.Errorf("mode %q: expected %q, got:\n%s", mode, want, printed)
		}
	}
}

func TestStatusOnTheHostThatKeepsTheOneDatabaseSaysWhenItLastBackedUp(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	at := statusNow.Add(-12 * time.Minute).UTC().Format(application.LastSyncFormat)
	if err := tracker.SetNote(context.Background(), application.LastBackupKey("vps"), at); err != nil {
		t.Fatal(err)
	}

	report := modeStatus(t, tracker, application.BeadsSyncBackup, nil)
	if want := "BEADS SYNC backup · backed up 12m ago"; !strings.Contains(report.String(), want) {
		t.Fatalf("expected %q, got:\n%s", want, report.String())
	}
	if !report.LastBackup.Equal(statusNow.Add(-12 * time.Minute)) {
		t.Fatalf("expected the last backup read back, got %s", report.LastBackup)
	}
}

func TestStatusSaysABackupNoteNobodyCanReadAsATimeIsUnreadable(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	if err := tracker.SetNote(context.Background(), application.LastBackupKey("vps"), "yesterday-ish"); err != nil {
		t.Fatal(err)
	}
	if want := "BEADS SYNC backup · last backup unreadable"; !strings.Contains(modeStatus(t, tracker, application.BeadsSyncBackup, nil).String(), want) {
		t.Fatalf("expected %q", want)
	}
}

func TestStatusReadsAnotherHostsNoteAsLiveWhenEveryHostReadsTheOneDatabase(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.31", "Something pathed to the laptop", "laptop")
	syncedAt(t, tracker, "laptop", 28*time.Hour)

	remote := section(modeStatus(t, tracker, application.BeadsSyncRemote, nil))
	if !strings.Contains(remote, "(a cycle behind)") {
		t.Fatalf("expected a remote copy's note read as a cycle behind, got:\n%s", remote)
	}
	for _, mode := range []application.BeadsSyncMode{application.BeadsSyncBackup, application.BeadsSyncShared} {
		block := section(modeStatus(t, tracker, mode, nil))
		if strings.Contains(block, "a cycle behind") {
			t.Errorf("mode %s: expected the note read live out of the one database, got:\n%s", mode, block)
		}
		if !strings.Contains(block, "last sync 2026-09-17T08:00:00Z") {
			t.Errorf("mode %s: expected the last sync still shown, got:\n%s", mode, block)
		}
	}
}

func TestStatusOnTheHostThatKeepsTheOneDatabaseCallsItsHaltABackupHalt(t *testing.T) {
	marker := apptest.NewFakeSyncHaltMarker()
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: statusNow.Add(-40 * time.Minute), Said: "conflict"}); err != nil {
		t.Fatal(err)
	}
	printed := modeStatus(t, aTrackerPathedToVPS(t), application.BeadsSyncBackup, marker).String()
	if !strings.Contains(printed, "host vps: beads backup halted since") {
		t.Fatalf("expected the halt named as the backup's, got:\n%s", printed)
	}
}

func TestStatusSyncModeLinesFitAPhone(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	if err := tracker.SetNote(context.Background(), application.LastBackupKey("vps"),
		statusNow.Add(-49*time.Hour).UTC().Format(application.LastSyncFormat)); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []application.BeadsSyncMode{application.BeadsSyncRemote, application.BeadsSyncBackup, application.BeadsSyncShared} {
		for _, line := range strings.Split(modeStatus(t, tracker, mode, nil).String(), "\n") {
			if n := utf8.RuneCountInString(line); n > application.Width {
				t.Errorf("mode %s: %d columns in %q", mode, n, line)
			}
		}
	}
}

func TestNudgeOnAHostThatReadsTheOneDatabaseKeepsOtherHostsAgesBesideItsOwnHalt(t *testing.T) {
	for mode, word := range map[application.BeadsSyncMode]string{
		application.BeadsSyncBackup: "beads backup halted",
		application.BeadsSyncShared: "own sync halted",
	} {
		t.Run(string(mode), func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.31", "Something pathed to the laptop", "laptop")
			claim(t, tracker, "mw-gq6.31", 5*time.Minute)
			syncedAt(t, tracker, "laptop", 31*time.Minute)
			marker := apptest.NewFakeSyncHaltMarker()
			if err := marker.Write(context.Background(), application.SyncHaltInfo{At: statusNow.Add(-15 * time.Minute), Said: "conflict"}); err != nil {
				t.Fatal(err)
			}

			clauses := nudgeReport(t, tracker, application.Nudge{SyncHalt: marker, SyncMode: mode})

			if len(clauses) != 2 {
				t.Fatalf("expected this host's halt and the laptop's age, got %+v", clauses)
			}
			if clauses[0].Key != "sync:vps" || !strings.Contains(clauses[0].Text, word) || strings.Contains(clauses[0].Text, "stale") {
				t.Fatalf("expected the halt said as %q, not calling other hosts' ages stale, got %+v", word, clauses[0])
			}
			if clauses[1].Key != "host:laptop" || clauses[1].Text != "laptop last synced 31 min ago" {
				t.Fatalf("expected the laptop's live age kept, got %+v", clauses[1])
			}
		})
	}
}

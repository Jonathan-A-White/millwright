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

// The Laptop's beads stayed at their 4 GB budget after a GC (mw-gq6.455): what
// filled the store was not garbage but the database's own history, one commit
// of about 35 KB for every bd write, 96,100 of them in four weeks, all of it
// reachable and so all of it kept by any GC. Only folding that history away
// gives the space back, and that is safe only where nothing can pull the old
// history back in: on the host that keeps the one database, while its backup
// is not getting through.

// keepingAHistory is the home with a long history, its backup down since well
// before the last day, and the backup due this sync halting the way the
// Laptop's has since 2026-10-05.
func keepingAHistory(t *testing.T, commits int) (application.Sync, *apptest.FakeTracker) {
	t.Helper()
	sync, _, tracker := keeping(t)
	sync.History = tracker
	sync.Squash = true
	tracker.SetHistory(commits)
	backedUpAgo(t, tracker, 10*24*time.Hour)
	tracker.SyncExits(1, "Blob not found ecbmjej2q1bv3qjpr9ihoj3ql22n5jrf.darc")
	return sync, tracker
}

func TestTheHomeSquashesALongHistoryWhileItsBackupIsDown(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a halted backup not to fail the sync, got %v", err)
	}
	if got := tracker.Squashes(); got != 1 {
		t.Fatalf("expected the history to be squashed once, got %d", got)
	}
	if report.Squashed != 96_100 {
		t.Fatalf("expected the report to count the 96100 commits squashed, got %d", report.Squashed)
	}
	if !report.GCed || tracker.GCs() != 1 {
		t.Fatalf("expected the day's collection to run as well, got GCed=%v and %d", report.GCed, tracker.GCs())
	}
	if line := report.String(); !strings.Contains(line, "squashed 96100 commits of beads history") {
		t.Fatalf("expected the sync's line to say what was squashed, got %q", line)
	}
}

func TestTheHomeLeavesAHistoryUnderTheThresholdAlone(t *testing.T) {
	sync, tracker := keepingAHistory(t, application.DefaultSquashAt-1)

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if tracker.Squashes() != 0 || report.Squashed != 0 {
		t.Fatalf("expected a history under %d commits to be kept, got %d squashes", application.DefaultSquashAt, tracker.Squashes())
	}
	if !report.GCed {
		t.Fatal("expected the day's collection to run all the same")
	}
}

func TestSquashAtSetsTheThreshold(t *testing.T) {
	sync, tracker := keepingAHistory(t, 1_000)
	sync.SquashAt = 500

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got := tracker.Squashes(); got != 1 {
		t.Fatalf("expected a history past SquashAt to be squashed, got %d", got)
	}
}

// A backup that gets through holds the old history: squashed here, its next
// pull would merge every old commit back in, or halt on a conflict for every
// row changed since (tried on a copy, mw-gq6.455). So a home whose backup is
// working keeps its history.
func TestTheHomeKeepsItsHistoryWhileItsBackupGetsThrough(t *testing.T) {
	cases := map[string]func(*testing.T) (application.Sync, *apptest.FakeTracker){
		"backed up this sync": func(t *testing.T) (application.Sync, *apptest.FakeTracker) {
			sync, _, tracker := keeping(t)
			sync.History = tracker
			sync.Squash = true
			tracker.SetHistory(96_100)
			backedUpAgo(t, tracker, 10*24*time.Hour)
			return sync, tracker
		},
		"backed up within the day": func(t *testing.T) (application.Sync, *apptest.FakeTracker) {
			sync, _, tracker := keeping(t)
			sync.History = tracker
			sync.Squash = true
			tracker.SetHistory(96_100)
			backedUpAgo(t, tracker, 10*time.Minute)
			return sync, tracker
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			sync, tracker := build(t)

			report, err := sync.Run(context.Background())
			if err != nil {
				t.Fatalf("syncing: %v", err)
			}
			if tracker.Squashes() != 0 || report.Squashed != 0 {
				t.Fatalf("expected a home with a working backup to keep its history, got %d squashes", tracker.Squashes())
			}
			if !report.GCed {
				t.Fatal("expected the day's collection to run all the same")
			}
		})
	}
}

// Every host that keeps its own copy brings it level with the others through
// the remote: one host folding its history would set every other copy at odds
// with it. Only the host that keeps the one database squashes.
func TestAHostThatSyncsThroughTheRemoteNeverSquashes(t *testing.T) {
	sync, _, tracker := syncing(t)
	sync.History = tracker
	sync.Squash = true
	tracker.SetHistory(96_100)

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if tracker.Squashes() != 0 || report.Squashed != 0 {
		t.Fatalf("expected a remote-mode host never to squash, got %d", tracker.Squashes())
	}
}

func TestTheHomeSquashesOnlyOnTheCollectionsCadence(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	if err := tracker.SetNote(context.Background(), application.LastGCKey("vps"),
		level.Add(-time.Hour).UTC().Format(application.LastSyncFormat)); err != nil {
		t.Fatalf("seeding the last collection: %v", err)
	}

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got := tracker.Squashes(); got != 0 {
		t.Fatalf("expected no squash before the collection is due, got %d", got)
	}
}

func TestASquashThatFailsLeavesTheCollectionAndTheSyncAlone(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	tracker.SquashErr = errors.New("bd sql: connection refused")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a failed squash not to fail the sync, got %v", err)
	}
	if report.Squashed != 0 {
		t.Fatalf("expected a failed squash not to be reported as done, got %d", report.Squashed)
	}
	if !report.GCed || tracker.GCs() != 1 {
		t.Fatalf("expected the collection to run after a failed squash, got GCed=%v and %d", report.GCed, tracker.GCs())
	}
}

func TestAHistoryThatCannotBeCountedIsNotSquashed(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	tracker.HistoryErr = errors.New("bd sql is not supported in embedded mode")

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got := tracker.Squashes(); got != 0 {
		t.Fatalf("expected no squash on a history nobody could count, got %d", got)
	}
}

// A vault blocked by somebody's uncommitted work still runs the home's beads
// half (mw-gq6.449), and its collection with it: the squash goes with the
// collection.
func TestABlockedVaultStillSquashesTheHomesHistory(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	files := &apptest.FakeVaultFiles{Dirty: []string{"seats/mayor/ledger.md"}}
	sync.Vault = files

	report, err := sync.Run(context.Background())
	if _, blocked := application.Blocked(err); !blocked {
		t.Fatalf("expected the vault to be reported blocked, got %v", err)
	}
	if got := tracker.Squashes(); got != 1 || report.Squashed != 96_100 {
		t.Fatalf("expected the squash to run beside the collection, got %d (reported %d)", got, report.Squashed)
	}
}

func TestANilHistoryNeverSquashes(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	sync.History = nil

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got := tracker.Squashes(); got != 0 {
		t.Fatalf("expected a sync with no history port never to squash, got %d", got)
	}
}

// The squash erases the only copy of the home's beads history while its backup
// is down, and only the Governor may say that is wanted (mw-gq6.504): a home
// whose config says nothing about it never squashes, however long the history
// and however long the backup has been down.
func TestTheHomeNeverSquashesUntilTheConfigTurnsItOn(t *testing.T) {
	sync, _, tracker := keeping(t)
	sync.History = tracker
	tracker.SetHistory(96_100)
	backedUpAgo(t, tracker, 10*24*time.Hour)
	tracker.SyncExits(1, "Blob not found ecbmjej2q1bv3qjpr9ihoj3ql22n5jrf.darc")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if tracker.Squashes() != 0 || report.Squashed != 0 {
		t.Fatalf("expected a home whose config does not turn the squash on to keep its history, got %d squashes", tracker.Squashes())
	}
	if line := report.String(); strings.Contains(line, "squashed") {
		t.Fatalf("expected the sync's line to say nothing of a squash, got %q", line)
	}
	if !report.GCed || tracker.GCs() != 1 {
		t.Fatalf("expected the day's collection to run all the same, got GCed=%v and %d", report.GCed, tracker.GCs())
	}
}

// The home's beads sat past their budget for days while every daily collection
// "succeeded" (mw-gq6.615): a collection that cannot give the space back must
// at least say so. A squash that fails leaves the history as it was, and the
// sync's line says why.
func TestASquashThatFailsIsSaidAndLeavesTheHistory(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	tracker.SquashErr = errors.New("bd sql: connection refused")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a failed squash not to fail the sync, got %v", err)
	}
	if report.SquashErr == nil || report.SquashErr.Error() != "bd sql: connection refused" {
		t.Fatalf("expected the report to carry why the squash failed, got %v", report.SquashErr)
	}
	if commits, _ := tracker.HistoryCommits(context.Background()); commits != 96_100 {
		t.Fatalf("expected a failed squash to leave the history untouched, got %d commits", commits)
	}
	if line := report.String(); !strings.Contains(line, "beads history not squashed: bd sql: connection refused") {
		t.Fatalf("expected the sync's line to say the squash failed, got %q", line)
	}
}

// A collection that fails is said on the sync's line, and not recorded as
// done: the next sync tries it again.
func TestACollectionThatFailsIsSaidAndTriedAgain(t *testing.T) {
	sync, tracker := keepingAHistory(t, 96_100)
	tracker.GCErr = errors.New("bd gc: database is locked")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a failed collection not to fail the sync, got %v", err)
	}
	if report.GCed || report.GCErr == nil {
		t.Fatalf("expected the report to carry the failed collection, got GCed=%v GCErr=%v", report.GCed, report.GCErr)
	}
	if line := report.String(); !strings.Contains(line, "garbage collection failed: bd gc: database is locked") {
		t.Fatalf("expected the sync's line to say the collection failed, got %q", line)
	}
	if last, _ := tracker.Note(context.Background(), application.LastGCKey("vps")); last != "" {
		t.Fatalf("expected a failed collection not to be recorded as done, got %q", last)
	}
}

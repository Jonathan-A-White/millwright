package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// level is the time the tests pin a sync to, so that the note a host leaves can
// be compared exactly.
var level = time.Date(2026, 9, 18, 14, 30, 0, 0, time.UTC)

// syncing is a sync of a clean vault and a tracker that has nothing to say,
// with the clock pinned.
func syncing(t *testing.T) (application.Sync, *apptest.FakeVaultFiles, *apptest.FakeTracker) {
	t.Helper()
	files := &apptest.FakeVaultFiles{}
	tracker := apptest.NewFakeTracker()
	return application.Sync{
		Vault:   files,
		Tracker: tracker,
		Host:    "vps",
		Now:     func() time.Time { return level },
		Sleep:   func(context.Context, time.Duration) error { return nil },
	}, files, tracker
}

func TestSyncMarksPullsPushesAndRecordsWhenItWasLevel(t *testing.T) {
	sync, files, tracker := syncing(t)
	files.Marked = true
	files.Incoming, files.Outgoing = 2, 1

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.Marked || report.Pulled != 2 || report.Pushed != 1 {
		t.Fatalf("expected a marked vault with 2 pulled and 1 pushed, got %+v", report)
	}
	if report.Quiet() {
		t.Fatal("expected a sync that moved commits not to be quiet")
	}
	if !report.At.Equal(level) {
		t.Fatalf("expected the sync to be level at %s, got %s", level, report.At)
	}

	note, err := tracker.Note(context.Background(), application.LastSyncKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	if note != level.Format(time.RFC3339) {
		t.Fatalf("expected host.vps.last_sync to be %q, got %q", level.Format(time.RFC3339), note)
	}
	if got := tracker.Syncs(); got != 1 {
		t.Fatalf("expected one synchronisation cycle, got %d", got)
	}
}

func TestSyncPublishesTheNoteInTheSameCycleThatWroteIt(t *testing.T) {
	sync, _, tracker := syncing(t)

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if got, want := tracker.PublishedNote(application.LastSyncKey("vps")), level.Format(application.LastSyncFormat); got != want {
		t.Fatalf("expected the other host to read host.vps.last_sync as %q after one sync, got %q", want, got)
	}
}

func TestAHaltedSyncTakesTheNoteBackToWhatItWas(t *testing.T) {
	key := application.LastSyncKey("vps")
	earlier := level.Add(-time.Hour).Format(application.LastSyncFormat)

	for name, before := range map[string]string{"a first sync": "", "a later one": earlier} {
		t.Run(name, func(t *testing.T) {
			sync, _, tracker := syncing(t)
			if before != "" {
				if err := tracker.SetNote(context.Background(), key, before); err != nil {
					t.Fatalf("writing the earlier note: %v", err)
				}
			}
			tracker.SyncExits(2, "conflict")

			if _, err := sync.Run(context.Background()); err == nil {
				t.Fatal("expected the halt to stop the sync")
			}
			note, err := tracker.Note(context.Background(), key)
			if err != nil {
				t.Fatalf("reading the note: %v", err)
			}
			if note != before {
				t.Fatalf("expected host.vps.last_sync to be back to %q, got %q", before, note)
			}
		})
	}
}

func TestSyncWithNothingToDoIsQuiet(t *testing.T) {
	sync, _, _ := syncing(t)

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.Quiet() {
		t.Fatalf("expected a quiet sync, got %+v", report)
	}
	if !strings.Contains(report.String(), "already level") {
		t.Fatalf("expected the report to say the vault was already level, got %q", report.String())
	}
}

func TestSyncMarksLedgersBeforeItPulls(t *testing.T) {
	sync, files, _ := syncing(t)
	files.PullErr = errors.New("the remote went away")

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected a pull that failed to stop the sync")
	}
	marks, pulls, pushes := files.Moves()
	if marks != 1 || pulls != 0 || pushes != 0 {
		t.Fatalf("expected the vault marked once and nothing pushed, got %d marks, %d pulls, %d pushes", marks, pulls, pushes)
	}
}

func TestSyncLeavesABlockedVaultAloneAndSyncsBeadsAnyway(t *testing.T) {
	sync, files, tracker := syncing(t)
	files.Dirty = []string{"seats/mayor/ledger.md", "seats/builder/rigs/millwright.md"}

	report, err := sync.Run(context.Background())
	if err == nil {
		t.Fatal("expected a vault holding uncommitted work to block the vault half of the sync")
	}
	blocked, stopped := application.Blocked(err)
	if !stopped {
		t.Fatalf("expected a blocked vault, got %T: %v", err, err)
	}
	if blocked.Host != "vps" {
		t.Fatalf("expected the blocked vault to name the host, got %q", blocked.Host)
	}
	if len(blocked.Files) != 2 {
		t.Fatalf("expected both files in the way, got %v", blocked.Files)
	}
	for _, want := range []string{"uncommitted", "seats/mayor/ledger.md", "seats/builder/rigs/millwright.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected the message to say %q, got %q", want, err)
		}
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("expected one line a timer can log, got %q", err)
	}
	if got := application.ExitStatus(err); got != application.VaultBlockedExit {
		t.Fatalf("expected mw to leave with %d, got %d", application.VaultBlockedExit, got)
	}

	if marks, pulls, pushes := files.Moves(); marks+pulls+pushes != 0 {
		t.Fatalf("expected the vault left alone, got %d marks, %d pulls, %d pushes", marks, pulls, pushes)
	}
	if got := tracker.Syncs(); got != 1 {
		t.Fatalf("expected the beads half to run anyway, got %d cycles", got)
	}

	if !report.At.IsZero() {
		t.Fatalf("expected no time to be recorded for a host that is not level, got %s", report.At)
	}
	note, err := tracker.Note(context.Background(), application.LastSyncKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	if note != "" {
		t.Fatalf("expected nothing recorded under host.vps.last_sync, got %q", note)
	}
	if len(report.Blocked) != 2 {
		t.Fatalf("expected the report to hold what blocked it, got %v", report.Blocked)
	}
	if report.Quiet() {
		t.Fatal("expected a blocked sync not to be called quiet")
	}
	if said := report.String(); !strings.Contains(said, "seats/mayor/ledger.md") || !strings.Contains(said, "beads synced") {
		t.Fatalf("expected the report to name the files and say beads were synced, got %q", said)
	}
}

func TestABlockedVaultHasAStatusOfItsOwn(t *testing.T) {
	for _, taken := range []int{0, 1, 2, 3, 4} {
		if application.VaultBlockedExit == taken {
			t.Fatalf("expected a status distinct from a plain failure and from bd's own, got %d", taken)
		}
	}
}

func TestExitStatusSaysWhatStoppedMw(t *testing.T) {
	if got := application.ExitStatus(nil); got != 0 {
		t.Fatalf("expected nothing wrong to leave with 0, got %d", got)
	}
	if got := application.ExitStatus(errors.New("something else went wrong")); got != 1 {
		t.Fatalf("expected an ordinary failure to leave with 1, got %d", got)
	}
	for code, want := range map[int]int{0: 1, 1: 1, 2: 2, 3: 3, 4: 4} {
		if got := application.ExitStatus(&application.SyncHalt{Code: code}); got != want {
			t.Fatalf("expected a halt on %d to leave with %d, got %d", code, want, got)
		}
	}
	blocked := &application.VaultBlocked{Host: "vps", Files: []string{"seats/mayor/ledger.md"}}
	if got := application.ExitStatus(fmt.Errorf("dispatching on vps: %w", blocked)); got != application.VaultBlockedExit {
		t.Fatalf("expected a blocked vault to leave with %d even when it is wrapped, got %d", application.VaultBlockedExit, got)
	}
}

func TestALocalNetworkFaultHasAStatusOfItsOwn(t *testing.T) {
	for _, taken := range []int{0, 1, 2, 3, 4, application.VaultBlockedExit, application.MillhandUpExit, application.WatchWakeExit} {
		if application.NetworkFaultExit == taken {
			t.Fatalf("expected a status distinct from every other mw leaves with, got %d", taken)
		}
	}
	fault := &application.LocalNetworkFault{Said: "Could not resolve host: github.com", Tries: 3}
	if got := application.ExitStatus(fmt.Errorf("dispatching: %w", fault)); got != application.NetworkFaultExit {
		t.Fatalf("expected a local network fault to leave with %d, got %d", application.NetworkFaultExit, got)
	}
	if want := "local network fault: Could not resolve host: github.com; nothing dispatched"; fault.Line() != want {
		t.Fatalf("expected the line %q, got %q", want, fault.Line())
	}
}

// A name that could not be resolved is still whatever failed underneath, so a
// sync run by hand leaves with the status it always did.
func TestAnUnresolvedNameKeepsTheHaltUnderneathItsExitCode(t *testing.T) {
	halted := &application.NameNotResolved{Said: "no such host", Err: &application.SyncHalt{Code: 3}}
	if got := application.ExitStatus(halted); got != 3 {
		t.Fatalf("expected the halt underneath to decide the status, got %d", got)
	}
	if got := application.ExitStatus(&application.NameNotResolved{Said: "no such host"}); got != 1 {
		t.Fatalf("expected a name that could not be resolved, run by hand, to be a plain failure, got %d", got)
	}
}

func TestSyncStopsWhenBeadsHaltsEvenThoughTheVaultWasBlockedToo(t *testing.T) {
	sync, files, tracker := syncing(t)
	files.Dirty = []string{"seats/mayor/ledger.md"}
	tracker.SyncExits(2, "bd said so")

	_, err := sync.Run(context.Background())
	halt, stopped := application.Halted(err)
	if !stopped {
		t.Fatalf("expected the beads halt to be what stopped the sync, got %T: %v", err, err)
	}
	if halt.Code != 2 {
		t.Fatalf("expected bd's own exit 2, got %d", halt.Code)
	}
	if got := application.ExitStatus(err); got != 2 {
		t.Fatalf("expected mw to leave with 2, got %d", got)
	}
	if got := tracker.Syncs(); got != 2 {
		t.Fatalf("expected the conflict's one retry even though the vault was blocked, got %d cycles", got)
	}
}

// A merge conflict is the one exit code that gets a retry: TestASyncConflict*
// below covers it. Every other halt is surfaced on the first try.
func TestSyncSurfacesAHaltAndNeverRetriesIt(t *testing.T) {
	for _, halt := range []struct {
		code int
		says []string
	}{
		{4, []string{"stuck", "nothing was pushed"}},
	} {
		sync, _, tracker := syncing(t)
		tracker.SyncExits(halt.code, "bd said so")

		_, err := sync.Run(context.Background())
		if err == nil {
			t.Fatalf("expected exit %d to stop the sync", halt.code)
		}
		for _, want := range append(halt.says, "bd said so") {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected the failure of exit %d to say %q, got %q", halt.code, want, err)
			}
		}
		stopped, ok := application.Halted(err)
		if !ok {
			t.Fatalf("expected exit %d to come back as a halt, got %T", halt.code, err)
		}
		if stopped.Code != halt.code {
			t.Fatalf("expected the halt to carry exit %d, got %d", halt.code, stopped.Code)
		}
		if stopped.Transient() {
			t.Fatalf("expected exit %d not to be called transient", halt.code)
		}
		if got := tracker.Syncs(); got != 1 {
			t.Fatalf("expected exit %d to be synced once and never retried, got %d cycles", halt.code, got)
		}
		note, _ := tracker.Note(context.Background(), application.LastSyncKey("vps"))
		if note != "" {
			t.Fatalf("expected a halted sync to record no time, got %q", note)
		}
	}
}

func TestAConflictThatClearsOnRetryIsLevelWithTheNotice(t *testing.T) {
	sync, _, tracker := syncing(t)
	var waited []time.Duration
	sync.Sleep = func(_ context.Context, d time.Duration) error {
		waited = append(waited, d)
		return nil
	}
	tracker.SyncExitsOnce(2, "conflict in the working set")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected the conflict to clear on retry, got %v", err)
	}
	if want := "conflict cleared on retry (bd said: conflict in the working set)"; report.Retried != want {
		t.Fatalf("expected the retry notice %q, got %q", want, report.Retried)
	}
	if !strings.Contains(report.String(), report.Retried) {
		t.Fatalf("expected the printed report to carry the notice, got %q", report.String())
	}
	if !report.At.Equal(level) {
		t.Fatalf("expected the sync to be level at %s, got %s", level, report.At)
	}
	if got := tracker.Syncs(); got != 2 {
		t.Fatalf("expected the conflict retried exactly once, got %d cycles", got)
	}
	if len(waited) != 1 || waited[0] != application.ConflictRetryDelay {
		t.Fatalf("expected one wait of %s before the retry, got %v", application.ConflictRetryDelay, waited)
	}
	note, err := tracker.Note(context.Background(), application.LastSyncKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	if note != level.Format(application.LastSyncFormat) {
		t.Fatalf("expected host.vps.last_sync to be recorded once the retry cleared, got %q", note)
	}
}

func TestAConflictThatDoesNotClearOnRetryHaltsCarryingBothFirstLines(t *testing.T) {
	sync, _, tracker := syncing(t)
	tracker.SyncExitsOnce(2, "first conflict")
	tracker.SyncExits(2, "second conflict")

	_, err := sync.Run(context.Background())
	halt, stopped := application.Halted(err)
	if !stopped {
		t.Fatalf("expected the halt to survive the retry, got %T: %v", err, err)
	}
	if halt.Code != 2 {
		t.Fatalf("expected bd's own exit 2, got %d", halt.Code)
	}
	for _, want := range []string{"first conflict", "second conflict"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected the halt to carry both first lines, missing %q in %q", want, err)
		}
	}
	if got := tracker.Syncs(); got != 2 {
		t.Fatalf("expected exactly one retry, got %d cycles", got)
	}
	note, _ := tracker.Note(context.Background(), application.LastSyncKey("vps"))
	if note != "" {
		t.Fatalf("expected a halt that survives the retry to record no time, got %q", note)
	}
}

func TestSyncCallsALostPushRaceTransient(t *testing.T) {
	sync, _, tracker := syncing(t)
	tracker.SyncExits(3, "")

	_, err := sync.Run(context.Background())
	stopped, ok := application.Halted(err)
	if !ok {
		t.Fatalf("expected exit 3 to come back as a halt, got %v", err)
	}
	if !stopped.Transient() {
		t.Fatal("expected a lost push race to be transient")
	}
	if got := tracker.Syncs(); got != 1 {
		t.Fatalf("expected mw not to retry the tracker's own retries, got %d cycles", got)
	}
}

func TestSyncNeedsAHostAndItsPorts(t *testing.T) {
	files, tracker := &apptest.FakeVaultFiles{}, apptest.NewFakeTracker()
	for what, sync := range map[string]application.Sync{
		"no host":    {Vault: files, Tracker: tracker},
		"no vault":   {Tracker: tracker, Host: "vps"},
		"no tracker": {Vault: files, Host: "vps"},
	} {
		if _, err := sync.Run(context.Background()); err == nil {
			t.Fatalf("expected a sync with %s to be refused", what)
		}
	}
}

func TestLastSyncKeyNamesTheHost(t *testing.T) {
	if got := application.LastSyncKey("vps"); got != "host.vps.last_sync" {
		t.Fatalf("expected host.vps.last_sync, got %q", got)
	}
	if got := application.LastSyncKey("laptop"); got != "host.laptop.last_sync" {
		t.Fatalf("expected host.laptop.last_sync, got %q", got)
	}
}

func TestLedgerMarkIsTheLineTheVaultNeeds(t *testing.T) {
	if application.LedgerMark != "seats/*/ledger.md merge=union" {
		t.Fatalf("expected the ledger mark to be the gitattributes line, got %q", application.LedgerMark)
	}
}

func TestSyncLeavesTheCountsOfItsTicksBesideTheNoteOfWhenItWasLevel(t *testing.T) {
	sync, _, tracker := syncing(t)
	sync.Ticks = application.TickLogs{
		Dispatch: heldLog(t, "2026-09-21T09:00:00Z ok: 1 started", "2026-09-21T09:15:00Z failed: no", "2026-09-21T09:30:00Z failed: no"),
	}
	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}

	note, err := tracker.Note(context.Background(), application.TicksKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	held := application.ParseHostTicks(note)
	if !held.Dispatch.Known || held.Dispatch.Failed != 2 || held.Millhand.Known {
		t.Fatalf("expected host.vps.ticks to hold 2 failed dispatch runs and nothing of the tick, got %q", note)
	}
	if key := application.TicksKey("vps"); key != "host.vps.ticks" {
		t.Fatalf("expected the key to sit beside host.vps.last_sync, got %q", key)
	}
}

func TestSyncWithNoLogsLeavesNoNoteOfTicks(t *testing.T) {
	sync, _, tracker := syncing(t)
	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if note, _ := tracker.Note(context.Background(), application.TicksKey("vps")); note != "" {
		t.Fatalf("expected no note of ticks without a log, got %q", note)
	}
}

func TestASyncThatCannotReadALogStillSyncs(t *testing.T) {
	sync, _, tracker := syncing(t)
	sync.Ticks = application.TickLogs{Dispatch: &apptest.FakeTickLog{ReadErr: errors.New("unreadable")}}
	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("expected a log nobody can read not to stop a sync, got %v", err)
	}
	if got := tracker.Syncs(); got != 1 {
		t.Fatalf("expected one cycle, got %d", got)
	}
}

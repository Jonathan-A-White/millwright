package application_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	stdsync "sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
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

// mw-gq6.98: a plain `mw sync` (contrib/mail-notify's step) never touched
// this host's own local sync-halted mark, only mw dispatch and mw millhand
// tick did — so a quiet alarm reading that mark saw nothing halted even while
// this host's own beads sync was stuck on a merge conflict. A sync's halt now
// writes it, the same shape as mw dispatch's own test.
func TestSyncMarksASyncHaltOnItsOwnLocalMarker(t *testing.T) {
	sync, _, tracker := syncing(t)
	marker := apptest.NewFakeSyncHaltMarker()
	sync.SyncHalts = marker
	tracker.SyncExits(2, "conflict in the working set")

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected the halt to stop the sync")
	}
	info, there, err := marker.Read(context.Background())
	if err != nil || !there {
		t.Fatalf("expected the marker written, there=%v, err=%v", there, err)
	}
	if !strings.Contains(info.Said, "conflict in the working set") {
		t.Fatalf("expected the marker to hold what bd said, got %+v", info)
	}
}

// TestSyncClearsItsOwnLocalMarkerOnceLevelAgain is the other half: a sync
// that gets level again clears the mark, so a later quiet alarm does not go
// on naming a halt that has already cleared.
func TestSyncClearsItsOwnLocalMarkerOnceLevelAgain(t *testing.T) {
	sync, _, _ := syncing(t)
	marker := apptest.NewFakeSyncHaltMarker()
	if err := marker.Write(context.Background(), application.SyncHaltInfo{At: level.Add(-time.Hour), Said: "old"}); err != nil {
		t.Fatalf("seeding the marker: %v", err)
	}
	sync.SyncHalts = marker

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("expected a level sync to succeed, got %v", err)
	}
	if _, there, _ := marker.Read(context.Background()); there {
		t.Fatal("expected a level sync to clear the marker")
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

func TestAHaltWorthAnAlarmNotesItselfForTheOtherHost(t *testing.T) {
	for _, code := range []int{2, 4} {
		sync, _, tracker := syncing(t)
		tracker.SyncExits(code, "bd said so")

		if _, err := sync.Run(context.Background()); err == nil {
			t.Fatalf("expected exit %d to stop the sync", code)
		}
		note, err := tracker.Note(context.Background(), application.SyncHaltKey("vps"))
		if err != nil {
			t.Fatalf("reading the note: %v", err)
		}
		info, ok := application.ParseSyncHalt(note)
		if !ok {
			t.Fatalf("expected a sync-halted note for exit %d, got %q", code, note)
		}
		if !info.At.Equal(level) || !strings.Contains(info.Said, "bd said so") {
			t.Fatalf("expected the note to hold %s and %q, got %+v", level, "bd said so", info)
		}
	}
}

func TestAHaltNotWorthAnAlarmLeavesNoNote(t *testing.T) {
	sync, _, tracker := syncing(t)
	tracker.SyncExits(3, "lost the push race")

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected exit 3 to stop the sync")
	}
	if note, _ := tracker.Note(context.Background(), application.SyncHaltKey("vps")); note != "" {
		t.Fatalf("expected no sync-halted note for a lost push race, got %q", note)
	}
}

func TestARepeatedHaltLeavesTheFirstNoteAlone(t *testing.T) {
	sync, _, tracker := syncing(t)
	earlier := level.Add(-time.Hour)
	if err := tracker.SetNote(context.Background(), application.SyncHaltKey("vps"),
		application.FormatSyncHalt(application.SyncHaltInfo{At: earlier, Said: "first conflict"})); err != nil {
		t.Fatalf("writing the earlier note: %v", err)
	}
	tracker.SyncExits(2, "second conflict")

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected the halt to stop the sync")
	}
	note, err := tracker.Note(context.Background(), application.SyncHaltKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	info, ok := application.ParseSyncHalt(note)
	if !ok || !info.At.Equal(earlier) || info.Said != "first conflict" {
		t.Fatalf("expected the note to keep naming the first halt, got %+v (ok=%v)", info, ok)
	}
}

func TestALevelSyncClearsTheHaltNote(t *testing.T) {
	sync, _, tracker := syncing(t)
	if err := tracker.SetNote(context.Background(), application.SyncHaltKey("vps"),
		application.FormatSyncHalt(application.SyncHaltInfo{At: level.Add(-time.Hour), Said: "old conflict"})); err != nil {
		t.Fatalf("writing the earlier note: %v", err)
	}

	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if note, _ := tracker.Note(context.Background(), application.SyncHaltKey("vps")); note != "" {
		t.Fatalf("expected a level sync to clear the sync-halted note, got %q", note)
	}
}

func TestABlockedVaultDoesNotClearTheHaltNote(t *testing.T) {
	sync, files, tracker := syncing(t)
	files.Dirty = []string{"seats/mayor/ledger.md"}
	if err := tracker.SetNote(context.Background(), application.SyncHaltKey("vps"),
		application.FormatSyncHalt(application.SyncHaltInfo{At: level.Add(-time.Hour), Said: "old conflict"})); err != nil {
		t.Fatalf("writing the earlier note: %v", err)
	}

	if _, err := sync.Run(context.Background()); err == nil {
		t.Fatal("expected a blocked vault to stop the sync from being level")
	}
	if note, _ := tracker.Note(context.Background(), application.SyncHaltKey("vps")); note == "" {
		t.Fatal("expected a sync that is not level to leave the sync-halted note alone")
	}
}

func TestSyncHaltKeyNamesTheHost(t *testing.T) {
	if got := application.SyncHaltKey("vps"); got != "host.vps.sync_halted" {
		t.Fatalf("expected host.vps.sync_halted, got %q", got)
	}
}

func TestFormatAndParseSyncHaltRoundTrip(t *testing.T) {
	info := application.SyncHaltInfo{At: level, Said: "the beads database has a merge conflict"}
	parsed, ok := application.ParseSyncHalt(application.FormatSyncHalt(info))
	if !ok || !parsed.At.Equal(info.At) || parsed.Said != info.Said {
		t.Fatalf("expected the mark to round-trip, got %+v (ok=%v)", parsed, ok)
	}
}

func TestParseSyncHaltRefusesTextWithNoTime(t *testing.T) {
	if _, ok := application.ParseSyncHalt("not a time\nsome text"); ok {
		t.Fatal("expected text with no time on its first line to read as no mark")
	}
	if _, ok := application.ParseSyncHalt(""); ok {
		t.Fatal("expected empty text to read as no mark")
	}
}

func TestRecordSyncHaltIsFreshOnlyOnce(t *testing.T) {
	marker := apptest.NewFakeSyncHaltMarker()
	halt := &application.SyncHalt{Code: 2, Said: "conflict"}

	if fresh := application.RecordSyncHalt(context.Background(), marker, halt, level); !fresh {
		t.Fatal("expected the first halt to be fresh")
	}
	if fresh := application.RecordSyncHalt(context.Background(), marker, halt, level.Add(time.Minute)); fresh {
		t.Fatal("expected a second halt to not be fresh")
	}
	info, there, err := marker.Read(context.Background())
	if err != nil || !there {
		t.Fatalf("expected the marker to hold the first halt, got %+v, there=%v, err=%v", info, there, err)
	}
	if !info.At.Equal(level) {
		t.Fatalf("expected the marker to keep naming the first halt at %s, got %s", level, info.At)
	}
}

func TestRecordSyncHaltIgnoresAHaltNotWorthAnAlarm(t *testing.T) {
	marker := apptest.NewFakeSyncHaltMarker()
	if fresh := application.RecordSyncHalt(context.Background(), marker, &application.SyncHalt{Code: 3}, level); fresh {
		t.Fatal("expected a lost push race not to be recorded")
	}
	if fresh := application.RecordSyncHalt(context.Background(), marker, errors.New("not a halt"), level); fresh {
		t.Fatal("expected an ordinary error not to be recorded")
	}
	if fresh := application.RecordSyncHalt(context.Background(), nil, &application.SyncHalt{Code: 2}, level); fresh {
		t.Fatal("expected a nil marker never to be called fresh")
	}
	if _, there, _ := marker.Read(context.Background()); there {
		t.Fatal("expected nothing recorded")
	}
}

func TestClearSyncHaltRemovesTheMarker(t *testing.T) {
	marker := apptest.NewFakeSyncHaltMarker()
	application.RecordSyncHalt(context.Background(), marker, &application.SyncHalt{Code: 2, Said: "x"}, level)

	application.ClearSyncHalt(context.Background(), marker)
	if _, there, _ := marker.Read(context.Background()); there {
		t.Fatal("expected the marker to be cleared")
	}
	// A nil marker must not panic.
	application.ClearSyncHalt(context.Background(), nil)
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

func TestASyncAsksTheTrackerToCollectWhenItHasNeverAsked(t *testing.T) {
	sync, _, tracker := syncing(t)

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.GCed {
		t.Fatal("expected a host that has never asked before to ask this time")
	}
	if got := tracker.GCs(); got != 1 {
		t.Fatalf("expected one collection, got %d", got)
	}
	if got := tracker.Repacked(); got != 1 {
		t.Fatalf("expected the due collection to also carry the git-remote-cache repack, got %d", got)
	}
	note, err := tracker.Note(context.Background(), application.LastGCKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	if note != level.Format(application.LastSyncFormat) {
		t.Fatalf("expected host.vps.last_gc to be %q, got %q", level.Format(application.LastSyncFormat), note)
	}
}

func TestASyncDoesNotAskTheTrackerToCollectBeforeItsCadence(t *testing.T) {
	sync, _, tracker := syncing(t)
	if err := tracker.SetNote(context.Background(), application.LastGCKey("vps"),
		level.Add(-time.Hour).UTC().Format(application.LastSyncFormat)); err != nil {
		t.Fatalf("seeding the last collection: %v", err)
	}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if report.GCed {
		t.Fatal("expected a host that collected an hour ago not to ask again before its cadence")
	}
	if got := tracker.GCs(); got != 0 {
		t.Fatalf("expected no collection, got %d", got)
	}
	if got := tracker.Repacked(); got != 0 {
		t.Fatalf("expected a GC held back by its cadence to carry no repack either, got %d", got)
	}
}

func TestASyncAsksTheTrackerToCollectOncePastItsCadence(t *testing.T) {
	sync, _, tracker := syncing(t)
	sync.GCInterval = time.Hour
	if err := tracker.SetNote(context.Background(), application.LastGCKey("vps"),
		level.Add(-2*time.Hour).UTC().Format(application.LastSyncFormat)); err != nil {
		t.Fatalf("seeding the last collection: %v", err)
	}

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("syncing: %v", err)
	}
	if !report.GCed {
		t.Fatal("expected a host past its cadence to ask again")
	}
	if got := tracker.GCs(); got != 1 {
		t.Fatalf("expected one collection, got %d", got)
	}
}

func TestASyncThatCannotCollectStillSucceeds(t *testing.T) {
	sync, _, tracker := syncing(t)
	tracker.GCErr = errors.New("no space to write a compacted commit")

	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a failed collection not to stop a sync, got %v", err)
	}
	if report.GCed {
		t.Fatal("expected a failed collection not to be reported as one")
	}
	if !strings.Contains(report.String(), "beads synced") {
		t.Fatalf("expected the sync to still report as synced, got %q", report.String())
	}
}

// span is when one beads cycle a delayedTracker ran started and ended.
type span struct{ start, end time.Time }

// delayedTracker wraps a FakeTracker but makes each Sync cycle take a
// measurable amount of real time, without holding the fake's own lock while
// it waits, and records when each cycle started and ended — so that a test of
// Sync.Lock can tell whether two callers' beads cycles overlapped, rather than
// only whether both got through.
type delayedTracker struct {
	*apptest.FakeTracker
	delay time.Duration

	mu     stdsync.Mutex
	cycles []span
}

func (d *delayedTracker) Sync(ctx context.Context) error {
	start := time.Now()
	time.Sleep(d.delay)
	err := d.FakeTracker.Sync(ctx)
	d.mu.Lock()
	d.cycles = append(d.cycles, span{start: start, end: time.Now()})
	d.mu.Unlock()
	return err
}

func TestTwoSyncsOnOneHostNeverInterleaveTheirBeadsCycle(t *testing.T) {
	lockDir := t.TempDir()
	lock := hostlock.New(lockDir, hostlock.WithPoll(5*time.Millisecond))
	tracker := &delayedTracker{FakeTracker: apptest.NewFakeTracker(), delay: 100 * time.Millisecond}

	newSync := func() application.Sync {
		return application.Sync{
			Vault:   &apptest.FakeVaultFiles{},
			Tracker: tracker,
			Host:    "vps",
			Lock:    lock,
			Now:     func() time.Time { return level },
		}
	}

	var wg stdsync.WaitGroup
	reports := make([]application.SyncReport, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reports[i], errs[i] = newSync().Run(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
		if !reports[i].At.Equal(level) {
			t.Fatalf("sync %d: expected to end level at %s, got %+v", i, level, reports[i])
		}
	}
	if got := tracker.FakeTracker.Syncs(); got != 2 {
		t.Fatalf("expected exactly two beads cycles, got %d", got)
	}
	if len(tracker.cycles) != 2 {
		t.Fatalf("expected two cycles recorded, got %d", len(tracker.cycles))
	}
	first, second := tracker.cycles[0], tracker.cycles[1]
	if first.start.Before(second.end) && second.start.Before(first.end) {
		t.Fatalf("expected the two cycles not to overlap, got %+v and %+v", first, second)
	}

	note, err := tracker.Note(context.Background(), application.LastSyncKey("vps"))
	if err != nil {
		t.Fatalf("reading the note: %v", err)
	}
	if note != level.UTC().Format(application.LastSyncFormat) {
		t.Fatalf("expected host.vps.last_sync to be %q, got %q", level.UTC().Format(application.LastSyncFormat), note)
	}
	if halted, _ := tracker.Note(context.Background(), application.SyncHaltKey("vps")); halted != "" {
		t.Fatalf("expected no halt, and so no restore, got %q", halted)
	}
}

func TestASyncLockHeldPastItsBoundNamesTheLockFile(t *testing.T) {
	lockDir := t.TempDir()
	holding, err := hostlock.New(lockDir, hostlock.WithWait(50*time.Millisecond), hostlock.WithPoll(5*time.Millisecond)).
		Take(context.Background())
	if err != nil {
		t.Fatalf("taking the lock to hold it: %v", err)
	}
	defer holding()

	sync, _, _ := syncing(t)
	sync.Lock = hostlock.New(lockDir, hostlock.WithWait(50*time.Millisecond), hostlock.WithPoll(5*time.Millisecond))

	_, err = sync.Run(context.Background())
	if err == nil {
		t.Fatal("expected a sync to fail while another already holds this host's sync lock")
	}
	if want := filepath.Join(lockDir, hostlock.File); !strings.Contains(err.Error(), want) {
		t.Fatalf("expected the failure to name the lock file %q, got %q", want, err)
	}
}

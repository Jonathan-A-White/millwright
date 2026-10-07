package eventlog_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/eventlog"
)

var ts = time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)

func mail(bead string) events.Event {
	return events.Event{Ts: ts, Kind: events.KindMail, Bead: bead, Actor: "mw@laptop", Detail: "mayor", Lane: events.LaneNormal}
}

func appendOK(t *testing.T, log *eventlog.Log, evs ...events.Event) uint64 {
	t.Helper()
	last, err := log.Append(context.Background(), evs)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	return last
}

func seqs(evs []events.Event) []uint64 {
	var out []uint64
	for _, e := range evs {
		out = append(out, e.Seq)
	}
	return out
}

func TestAnEmptyLogHasHeadZeroAndNothingSince(t *testing.T) {
	log := eventlog.New(filepath.Join(t.TempDir(), "events", "log.jsonl"))
	head, err := log.Head(context.Background())
	if err != nil || head != 0 {
		t.Fatalf("Head of no log = %d, %v; want 0, nil", head, err)
	}
	evs, err := log.Since(context.Background(), 0)
	if err != nil || len(evs) != 0 {
		t.Fatalf("Since(0) of no log = %v, %v; want nothing", evs, err)
	}
}

func TestAppendNumbersEventsOnFromTheHeadAndSinceReturnsOnlyLaterOnes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	log := eventlog.New(path)
	if last := appendOK(t, log, mail("mw-1"), mail("mw-2")); last != 2 {
		t.Fatalf("the first append ended at seq %d, want 2", last)
	}
	if last := appendOK(t, log, mail("mw-3")); last != 3 {
		t.Fatalf("the second append ended at seq %d, want 3", last)
	}
	// Another Log on the same file, as another process would open it.
	again := eventlog.New(path)
	if head, err := again.Head(context.Background()); err != nil || head != 3 {
		t.Fatalf("Head = %d, %v; want 3", head, err)
	}
	evs, err := again.Since(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := seqs(evs); len(got) != 2 || got[0] != 2 || got[1] != 3 || evs[1].Bead != "mw-3" {
		t.Fatalf("Since(1) = %+v, want seqs 2 and 3", evs)
	}
	if evs, _ := again.Since(context.Background(), 3); len(evs) != 0 {
		t.Fatalf("Since(head) = %+v, want nothing", evs)
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "log.seq")); err != nil || strings.TrimSpace(string(data)) != "3" {
		t.Fatalf("the .seq sidecar holds %q (%v), want 3", data, err)
	}
	lines := strings.Split(strings.TrimSpace(readFile(t, path)), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], `{"seq":1,`) {
		t.Fatalf("the log should be one JSON event per line, got:\n%s", readFile(t, path))
	}
}

func TestAppendRefusesAnInvalidEventAndWritesNothing(t *testing.T) {
	log := eventlog.New(filepath.Join(t.TempDir(), "log.jsonl"))
	bad := events.Event{Ts: ts, Kind: events.KindBeadChanged, Bead: "mw-1", From: "open", To: "landed", Lane: events.LaneNormal}
	if _, err := log.Append(context.Background(), []events.Event{mail("mw-1"), bad}); err == nil || !strings.Contains(err.Error(), `no transition from "open" to "landed"`) {
		t.Fatalf("expected the bad transition refused, got %v", err)
	}
	if head, _ := log.Head(context.Background()); head != 0 {
		t.Fatalf("a refused batch moved the head to %d", head)
	}
}

func TestATornLastLineIsCutAndItsSeqIsNotReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	log := eventlog.New(path)
	appendOK(t, log, mail("mw-1"), mail("mw-2"))
	// A crash after the log's fsync but before the sidecar's: the sidecar
	// lags, and the log ends in half a line.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "log.seq"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"seq":3,"ts":"2026-`)
	f.Close()

	if last := appendOK(t, log, mail("mw-3")); last != 3 {
		t.Fatalf("the append after a torn line ended at seq %d, want 3", last)
	}
	evs, err := log.Since(context.Background(), 0)
	if err != nil {
		t.Fatalf("reading the log back: %v", err)
	}
	if got := seqs(evs); len(got) != 3 || got[2] != 3 || evs[2].Bead != "mw-3" {
		t.Fatalf("the log holds %+v, want seqs 1 2 3", evs)
	}
}

func TestTwoAppendersNeverShareASeq(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log := eventlog.New(path)
			for i := 0; i < 10; i++ {
				if _, err := log.Append(context.Background(), []events.Event{mail("mw-1")}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	evs, err := eventlog.New(path).Since(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 40 {
		t.Fatalf("40 appends left %d events", len(evs))
	}
	for i, e := range evs {
		if e.Seq != uint64(i+1) {
			t.Fatalf("event %d has seq %d: seqs must run 1..40 in order", i, e.Seq)
		}
	}
}

func TestTheCursorIsSavedBesideTheLogAndLoadedBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	cursors := eventlog.NewCursors(path)
	if _, ok, err := cursors.Load(context.Background()); err != nil || ok {
		t.Fatalf("Load with nothing saved = %v, %v; want not ok", ok, err)
	}
	want := application.FollowCursor{Since: ts, Seen: []string{"e1", "c1"}, States: map[string]string{"mw-1": "open"}}
	if err := cursors.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := eventlog.NewCursors(path).Load(context.Background())
	if err != nil || !ok || !got.Since.Equal(ts) || strings.Join(got.Seen, ",") != "e1,c1" || got.States["mw-1"] != "open" {
		t.Fatalf("Load = %+v, %v, %v; want %+v", got, ok, err, want)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "follow.json")); err != nil {
		t.Fatalf("expected the cursor in follow.json beside the log: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestShipStatesAreEmptyBeforeTheFirstSaveAndKeepWhatIsSaved(t *testing.T) {
	states := eventlog.NewShipStates(filepath.Join(t.TempDir(), "events", "log.jsonl"))
	got, err := states.Load(context.Background())
	if err != nil || got.Shipped != 0 || len(got.Pending) != 0 {
		t.Fatalf("expected the zero state before a save, got %+v %v", got, err)
	}
	want := application.ShipState{Shipped: 9, Pending: []application.ShipRange{{From: 4, To: 6}, {From: 7, To: 9}}, Day: "2026-10-01", Chain: 3, Alarmed: true}
	if err := states.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err = states.Load(context.Background())
	if err != nil || got.Shipped != 9 || len(got.Pending) != 2 || got.Pending[1] != want.Pending[1] || got.Day != want.Day || got.Chain != 3 || !got.Alarmed {
		t.Fatalf("expected %+v back, got %+v %v", want, got, err)
	}
}

func TestNudgeCursorsRoundTripBesideTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	c := eventlog.NewNudgeCursors(path)
	got, err := c.Load(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("no file: got %v, %v", got, err)
	}
	if err := c.Save(context.Background(), map[string]uint64{"mayor": 12, "deputy": 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "nudge.json")); err != nil {
		t.Fatalf("the cursors are not in nudge.json beside the log: %v", err)
	}
	got, err = eventlog.NewNudgeCursors(path).Load(context.Background())
	if err != nil || got["mayor"] != 12 || got["deputy"] != 7 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func newLogOf(t *testing.T, n int) (*eventlog.Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events", "log.jsonl")
	log := eventlog.New(path)
	for i := 0; i < n; i++ {
		appendOK(t, log, mail("mw-1"))
	}
	return log, path
}

func TestTrimMovesTheOldEventsToTheDatedArchiveAndKeepsTheTailAndTheSeq(t *testing.T) {
	log, path := newLogOf(t, 10)
	moved, err := log.Trim(context.Background(), 6, "2026-10-07")
	if err != nil || moved != 6 {
		t.Fatalf("Trim = %d, %v; want 6 moved", moved, err)
	}
	kept, _ := log.Since(context.Background(), 0)
	if got := seqs(kept); len(got) != 4 || got[0] != 7 || got[3] != 10 {
		t.Fatalf("the log holds seqs %v, want 7 to 10", got)
	}
	if first, _ := log.First(context.Background()); first != 7 {
		t.Fatalf("First = %d, want 7", first)
	}
	if head, _ := log.Head(context.Background()); head != 10 {
		t.Fatalf("Head = %d, want 10", head)
	}
	archive := filepath.Join(filepath.Dir(path), "archive", "log-2026-10-07.jsonl")
	if lines := strings.Split(strings.TrimSpace(readFile(t, archive)), "\n"); len(lines) != 6 || !strings.HasPrefix(lines[0], `{"seq":1,`) {
		t.Fatalf("the archive holds %d lines:\n%s", len(lines), readFile(t, archive))
	}
	if last := appendOK(t, log, mail("mw-2")); last != 11 {
		t.Fatalf("the append after a trim got seq %d, want 11", last)
	}
}

func TestTrimWithNothingToMoveLeavesTheLogAlone(t *testing.T) {
	log, path := newLogOf(t, 3)
	before := readFile(t, path)
	if moved, err := log.Trim(context.Background(), 0, "2026-10-07"); err != nil || moved != 0 {
		t.Fatalf("Trim = %d, %v; want nothing moved", moved, err)
	}
	if readFile(t, path) != before {
		t.Fatal("a trim that moved nothing changed the log")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "archive")); err == nil {
		t.Fatal("a trim that moved nothing made an archive")
	}
}

func TestTrimRepairsATornLastLine(t *testing.T) {
	log, path := newLogOf(t, 5)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"seq":6,"ts":"2026-`)
	f.Close()
	if moved, err := log.Trim(context.Background(), 3, "2026-10-07"); err != nil || moved != 3 {
		t.Fatalf("Trim = %d, %v; want 3 moved", moved, err)
	}
	if text := readFile(t, path); !strings.HasSuffix(text, "\n") || strings.Contains(text, `"seq":6`) {
		t.Fatalf("the torn line is still in the log:\n%s", text)
	}
	if last := appendOK(t, log, mail("mw-6")); last != 6 {
		t.Fatalf("the append after trimming a torn log got seq %d, want 6", last)
	}
	kept, err := log.Since(context.Background(), 0)
	if err != nil || len(kept) != 3 || kept[0].Seq != 4 {
		t.Fatalf("the log holds %v, %v; want seqs 4 to 6", seqs(kept), err)
	}
}

func TestTrimmingEverythingLeavesTheHeadInTheSidecar(t *testing.T) {
	log, _ := newLogOf(t, 3)
	if _, err := log.Trim(context.Background(), 3, "2026-10-07"); err != nil {
		t.Fatal(err)
	}
	if head, _ := log.Head(context.Background()); head != 3 {
		t.Fatalf("Head = %d, want 3", head)
	}
	if first, _ := log.First(context.Background()); first != 0 {
		t.Fatalf("First of an empty log = %d, want 0", first)
	}
	if last := appendOK(t, log, mail("mw-4")); last != 4 {
		t.Fatalf("the next seq is %d, want 4", last)
	}
}

func TestArchivedReadsEveryDatedFileInOrderAndOnlyAfterSeq(t *testing.T) {
	log, _ := newLogOf(t, 12)
	if _, err := log.Trim(context.Background(), 4, "2026-10-06"); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Trim(context.Background(), 8, "2026-10-07"); err != nil {
		t.Fatal(err)
	}
	old, err := log.Archived(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := seqs(old); len(got) != 6 || got[0] != 3 || got[5] != 8 {
		t.Fatalf("Archived(2) = %v, want 3 to 8", got)
	}
	if old, _ := log.Archived(context.Background(), 8); len(old) != 0 {
		t.Fatalf("Archived(8) = %v, want nothing", seqs(old))
	}
}

func TestATrimThatCrashedBeforeCuttingTheLogLeavesNoDuplicatesToRead(t *testing.T) {
	log, path := newLogOf(t, 6)
	if _, err := log.Trim(context.Background(), 3, "2026-10-07"); err != nil {
		t.Fatal(err)
	}
	// The same events archived again, as a retry after a crash would.
	archive := filepath.Join(filepath.Dir(path), "archive", "log-2026-10-07.jsonl")
	data := readFile(t, archive)
	if err := os.WriteFile(archive, []byte(data+data), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := log.Archived(context.Background(), 0)
	if err != nil || len(old) != 3 {
		t.Fatalf("Archived(0) = %v, %v; want seqs 1 to 3 once", seqs(old), err)
	}
}

func TestAppendsDuringTrimsAreNeverLost(t *testing.T) {
	log, path := newLogOf(t, 5)
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := eventlog.New(path)
			for i := 0; i < 20; i++ {
				if _, err := other.Append(context.Background(), []events.Event{mail("mw-9")}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 10; i++ {
		head, _ := log.Head(context.Background())
		if _, err := log.Trim(context.Background(), head/2, "2026-10-07"); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	old, _ := log.Archived(context.Background(), 0)
	kept, _ := log.Since(context.Background(), 0)
	if total := len(old) + len(kept); total != 65 {
		t.Fatalf("%d events archived and %d kept, want 65 in all", len(old), len(kept))
	}
}

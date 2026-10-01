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

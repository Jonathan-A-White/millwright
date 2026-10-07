package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

type trimWorld struct {
	log     *apptest.FakeEventLog
	cursors *apptest.FakeNudgeCursors
	ship    *apptest.FakeShipStates
	trim    application.EventTrim
}

// newTrimWorld is a log of n events, every cursor and the shipper at its
// head, keeping the newest 10.
func newTrimWorld(t *testing.T, n int) *trimWorld {
	t.Helper()
	w := &trimWorld{log: &apptest.FakeEventLog{}, cursors: &apptest.FakeNudgeCursors{}, ship: &apptest.FakeShipStates{}}
	for i := 0; i < n; i++ {
		appendAll(t, w.log, jobEvent)
	}
	w.cursors.Save(context.Background(), map[string]uint64{"deputy": uint64(n), application.ControlCursorKey: uint64(n)})
	w.ship.Save(context.Background(), application.ShipState{Shipped: uint64(n)})
	w.trim = application.EventTrim{
		Log: w.log, Archive: w.log, Nudges: w.cursors, Ship: w.ship, Keep: 10,
		Now: func() time.Time { return time.Date(2026, 10, 7, 23, 0, 0, 0, time.UTC) },
	}
	return w
}

func (w *trimWorld) first(t *testing.T) uint64 {
	t.Helper()
	first, err := w.log.First(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return first
}

func TestTrimMovesEverythingPassedButTheNewestKeptAndKeepsTheSeq(t *testing.T) {
	w := newTrimWorld(t, 25)
	got, err := w.trim.Run(context.Background())
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if got.Moved != 15 || got.UpTo != 15 {
		t.Fatalf("trim = %+v, want 15 moved up to seq 15", got)
	}
	if first := w.first(t); first != 16 {
		t.Fatalf("the log's first kept seq is %d, want 16", first)
	}
	if head, _ := w.log.Head(context.Background()); head != 25 {
		t.Fatalf("head = %d, want 25: numbering is unchanged", head)
	}
	if days := w.log.TrimDays(); len(days) != 1 || days[0] != "2026-10-07" {
		t.Fatalf("archived on days %v, want [2026-10-07]", days)
	}
	appendAll(t, w.log, jobEvent)
	if head, _ := w.log.Head(context.Background()); head != 26 {
		t.Fatalf("the next event after a trim got seq %d, want 26", head)
	}
}

func TestTrimStopsAtTheLowestCursor(t *testing.T) {
	w := newTrimWorld(t, 25)
	w.cursors.Save(context.Background(), map[string]uint64{"deputy": 25, "mayor": 7, application.ControlCursorKey: 20})
	got, err := w.trim.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.UpTo != 7 || w.first(t) != 8 {
		t.Fatalf("trim = %+v, first %d; want the cut at the mayor's cursor 7", got, w.first(t))
	}
}

func TestTrimStopsAtWhatIsShipped(t *testing.T) {
	w := newTrimWorld(t, 25)
	w.ship.Save(context.Background(), application.ShipState{Shipped: 4})
	got, err := w.trim.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.UpTo != 4 || w.first(t) != 5 {
		t.Fatalf("trim = %+v, first %d; want the cut at what is shipped, 4", got, w.first(t))
	}
}

func TestTrimKeepsABatchWaitingForTheChain(t *testing.T) {
	w := newTrimWorld(t, 25)
	w.ship.Save(context.Background(), application.ShipState{Shipped: 25, Pending: []application.ShipRange{{From: 9, To: 12}}})
	got, err := w.trim.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.UpTo != 8 || w.first(t) != 9 {
		t.Fatalf("trim = %+v, first %d; want the cut before the pending batch, at 8", got, w.first(t))
	}
}

func TestTrimMovesNothingFromALogNoLongerThanWhatItKeeps(t *testing.T) {
	w := newTrimWorld(t, 10)
	got, err := w.trim.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Moved != 0 || len(w.log.TrimDays()) != 0 || w.first(t) != 1 {
		t.Fatalf("trim = %+v, first %d, days %v; want nothing moved", got, w.first(t), w.log.TrimDays())
	}
}

func TestTrimDefaultsToKeepingTheNewest5000(t *testing.T) {
	w := newTrimWorld(t, 5001)
	w.trim.Keep = 0
	got, err := w.trim.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Moved != 1 || w.first(t) != 2 {
		t.Fatalf("trim = %+v, first %d; want 1 moved, 5000 kept", got, w.first(t))
	}
}

func TestTrimNeedsALogAnArchiveCursorsAndTheShipState(t *testing.T) {
	err := application.EventTrim{}.Trim(context.Background())
	if err == nil || !strings.Contains(err.Error(), "needs") {
		t.Fatalf("trim with nothing = %v, want it to say what it needs", err)
	}
}

func TestTailReadsTheArchiveWhenSinceIsOlderThanTheLogsFirstEvent(t *testing.T) {
	w := newTrimWorld(t, 25)
	if _, err := w.trim.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (application.EventTail{Log: w.log, Archive: w.log, Since: 12, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 13 || !strings.HasPrefix(lines[0], "13 ") || !strings.HasPrefix(lines[12], "25 ") {
		t.Fatalf("tail --since 12 printed %d lines:\n%s\nwant seqs 13 to 25, the first 3 from the archive", len(lines), out.String())
	}
}

// lossyArchive is an archive that lost the events from seq 14 on.
type lossyArchive struct{ *apptest.FakeEventLog }

func (l lossyArchive) Archived(ctx context.Context, seq uint64) ([]events.Event, error) {
	all, err := l.FakeEventLog.Archived(ctx, seq)
	var kept []events.Event
	for _, e := range all {
		if e.Seq < 14 {
			kept = append(kept, e)
		}
	}
	return kept, err
}

func TestTailSaysWhenTheArchiveLacksEventsItShouldHold(t *testing.T) {
	w := newTrimWorld(t, 25)
	if _, err := w.trim.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := (application.EventTail{Log: w.log, Archive: lossyArchive{w.log}, Since: 12, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "(events 14 to 15 are in neither the log nor the archive)") {
		t.Fatalf("tail printed:\n%s\nwant it to say events 14 to 15 are gone", out.String())
	}
}

func TestFollowTrimsAtMostOnceADay(t *testing.T) {
	trimmer := &countingTrimmer{}
	now := time.Date(2026, 10, 7, 23, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	f := application.EventFollow{
		Head: &fixedHead{}, Publish: func(context.Context) error { return nil },
		Trimmer: trimmer, Now: clock,
	}
	passes := 0
	f.Sleep = func(ctx context.Context, _ time.Duration) error {
		passes++
		switch passes {
		case 3:
			now = now.Add(2 * time.Hour) // past midnight UTC
		case 6:
			return context.Canceled
		}
		return nil
	}
	if err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if trimmer.n != 2 {
		t.Fatalf("trimmed %d times over six passes across a midnight, want 2", trimmer.n)
	}
}

type countingTrimmer struct{ n int }

func (c *countingTrimmer) Trim(context.Context) error { c.n++; return nil }

type fixedHead struct{}

func (*fixedHead) Head(context.Context) (string, error) { return "h", nil }

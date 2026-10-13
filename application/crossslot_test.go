package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// slotClock is a cross-host slot's clock in a test: a poll or a settle moves it
// on at once, after whatever the test wants done in that pause, and a renewal's
// wait blocks until the test lets it through.
type slotClock struct {
	mu     sync.Mutex
	now    time.Time
	renew  time.Duration
	paused func(d time.Duration) // run at each poll or settle, before the clock moves on

	waiting chan struct{} // told each time the renewal begins to wait
	beat    chan struct{} // lets one renewal through
}

func newSlotClock() *slotClock {
	return &slotClock{
		now:     time.Date(2026, 10, 10, 23, 0, 0, 0, time.UTC),
		waiting: make(chan struct{}, 16),
		beat:    make(chan struct{}),
	}
}

func (k *slotClock) Now() time.Time {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now
}

func (k *slotClock) Wait(ctx context.Context, d time.Duration) error {
	if d == k.renew {
		k.waiting <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-k.beat:
			return nil
		}
	}
	if k.paused != nil {
		k.paused(d)
	}
	k.mu.Lock()
	k.now = k.now.Add(d)
	k.mu.Unlock()
	return nil
}

const (
	slotTTL    = 9 * time.Minute // renewed every 3 minutes
	slotPoll   = time.Minute
	slotSettle = 5 * time.Second
)

func testSlot(notes application.SlotNotes, k *slotClock) application.CrossSlot {
	k.renew = slotTTL / 3
	return application.CrossSlot{Notes: notes, Host: "desktop", TTL: slotTTL, Poll: slotPoll, Settle: slotSettle, Now: k.Now, Wait: k.Wait}
}

func leaseOf(t *testing.T, tracker *apptest.FakeTracker, rig string) (application.CrossSlotLease, bool) {
	t.Helper()
	note, err := tracker.Note(context.Background(), application.CrossSlotKey(rig))
	if err != nil {
		t.Fatal(err)
	}
	return application.ReadCrossSlotLease(note)
}

func setLease(t *testing.T, tracker *apptest.FakeTracker, rig string, lease application.CrossSlotLease) {
	t.Helper()
	if err := tracker.SetNote(context.Background(), application.CrossSlotKey(rig), lease.Note()); err != nil {
		t.Fatal(err)
	}
}

func TestAFreeCrossSlotIsTakenSaidNothingOfAndGivenBack(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	held, said := testSlot(tracker, k).Take(context.Background(), "lampas", "builder@desktop closing out mw-y3qno5.3")
	if held == nil || len(said) != 0 {
		t.Fatalf("expected the slot taken with nothing to say, got %v %q", held, said)
	}
	lease, ok := leaseOf(t, tracker, "lampas")
	if !ok || lease.Holder != "builder@desktop closing out mw-y3qno5.3" || lease.Host != "desktop" || !lease.Expires.After(k.Now()) {
		t.Fatalf("expected the lease written for this holder, got %+v", lease)
	}
	if trouble := held.Release(context.Background()); len(trouble) != 0 {
		t.Fatalf("expected a clean release, got %q", trouble)
	}
	if _, ok := leaseOf(t, tracker, "lampas"); ok {
		t.Fatal("expected the slot given back")
	}
	if trouble := held.Release(context.Background()); len(trouble) != 0 {
		t.Fatalf("expected a second release to be harmless, got %q", trouble)
	}
}

func TestACrossSlotTheOtherHostHoldsIsWaitedForAndTheWaitNamed(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	setLease(t, tracker, "lampas", application.CrossSlotLease{
		Holder: "builder@laptop closing out mw-x.1", Host: "laptop", Token: "laptop-1", Taken: k.Now(), Expires: k.Now().Add(slotTTL),
	})
	polls := 0
	k.paused = func(d time.Duration) {
		if d == slotPoll {
			if polls++; polls == 27 {
				_ = tracker.ClearNote(context.Background(), application.CrossSlotKey("lampas"))
			}
		}
		// The laptop renews its lease while it lands.
		if lease, ok := leaseOf(t, tracker, "lampas"); ok && lease.Host == "laptop" {
			lease.Expires = k.Now().Add(slotTTL)
			setLease(t, tracker, "lampas", lease)
		}
	}
	held, said := testSlot(tracker, k).Take(context.Background(), "lampas", "builder@desktop closing out mw-y3qno5.3")
	if held == nil {
		t.Fatalf("expected the slot taken once the laptop gave it back, got %q", said)
	}
	defer held.Release(context.Background())
	if len(said) != 1 || said[0] != "waited 27m for laptop's landing of lampas" {
		t.Fatalf("expected the wait named, got %q", said)
	}
}

func TestACrossSlotWhoseLeaseRanOutIsTakenOverNamingTheHolderAndItsAge(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	setLease(t, tracker, "lampas", application.CrossSlotLease{
		Holder: "builder@laptop closing out mw-x.9", Host: "laptop", Token: "gone",
		Taken: k.Now().Add(-47 * time.Minute), Expires: k.Now().Add(-37 * time.Minute),
	})
	held, said := testSlot(tracker, k).Take(context.Background(), "lampas", "builder@desktop closing out mw-y3qno5.3")
	if held == nil {
		t.Fatalf("expected the slot taken over, got %q", said)
	}
	defer held.Release(context.Background())
	want := "took over the cross-host merge slot of lampas from builder@laptop closing out mw-x.9: its lease was taken 47m ago and ran out 37m ago, so its holder is gone"
	if len(said) != 1 || said[0] != want {
		t.Fatalf("expected %q, got %q", want, said)
	}
	if lease, ok := leaseOf(t, tracker, "lampas"); !ok || lease.Host != "desktop" {
		t.Fatalf("expected the lease now this host's, got %+v", lease)
	}
}

func TestACrossSlotHoldingSomethingThatIsNoLeaseIsTakenOverSayingSo(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	_ = tracker.SetNote(context.Background(), application.CrossSlotKey("lampas"), "not a lease")
	held, said := testSlot(tracker, k).Take(context.Background(), "lampas", "me")
	if held == nil || len(said) != 1 || !strings.Contains(said[0], `could not be read as a lease: "not a lease"`) {
		t.Fatalf("expected the slot taken over, saying what it held, got %v %q", held, said)
	}
	held.Release(context.Background())
}

func TestACrossSlotTheBeadsServerCannotGiveIsSkippedSayingWhy(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	tracker.Err = errors.New("dial tcp 127.0.0.1:3307: connect: connection refused")
	held, said := testSlot(tracker, k).Take(context.Background(), "lampas", "me")
	if held != nil {
		t.Fatal("expected no holding")
	}
	want := "the cross-host merge slot of lampas was skipped, so this landing went ahead under this host's merge slot alone: the beads server could not be reached: dial tcp 127.0.0.1:3307: connect: connection refused (3 tries failed over 2m)"
	if len(said) != 1 || said[0] != want {
		t.Fatalf("expected %q, got %q", want, said)
	}
	// Releasing what was never held is harmless.
	if trouble := held.Release(context.Background()); trouble != nil {
		t.Fatalf("expected nothing, got %q", trouble)
	}
}

func TestACloseOutThatLosesTheRaceToWriteTheLeaseWaitsForTheWinner(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	settles, polls := 0, 0
	k.paused = func(d time.Duration) {
		switch d {
		case slotSettle:
			// The laptop wrote its own lease in the same moment, after this one.
			if settles++; settles == 1 {
				setLease(t, tracker, "lampas", application.CrossSlotLease{
					Holder: "builder@laptop closing out mw-x.2", Host: "laptop", Token: "laptop-2", Taken: k.Now(), Expires: k.Now().Add(slotTTL),
				})
			}
		case slotPoll:
			if polls++; polls == 4 {
				_ = tracker.ClearNote(context.Background(), application.CrossSlotKey("lampas"))
			}
		}
	}
	held, said := testSlot(tracker, k).Take(context.Background(), "lampas", "me")
	if held == nil {
		t.Fatalf("expected the slot taken after the laptop, got %q", said)
	}
	defer held.Release(context.Background())
	if settles != 2 || len(said) != 1 || !strings.HasPrefix(said[0], "waited 4m") || !strings.HasSuffix(said[0], "for laptop's landing of lampas") {
		t.Fatalf("expected a second try after waiting for the laptop, got %d settles and %q", settles, said)
	}
}

func TestAHeldCrossSlotIsRenewedAndNeverWrittenOverOnceLost(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	held, _ := testSlot(tracker, k).Take(context.Background(), "lampas", "me")
	if held == nil {
		t.Fatal("expected the slot taken")
	}
	took, _ := leaseOf(t, tracker, "lampas")

	<-k.waiting
	k.mu.Lock()
	k.now = k.now.Add(slotTTL / 3)
	k.mu.Unlock()
	k.beat <- struct{}{}
	<-k.waiting
	renewed, _ := leaseOf(t, tracker, "lampas")
	if renewed.Token != took.Token || !renewed.Expires.After(took.Expires) {
		t.Fatalf("expected the lease renewed, got %+v after %+v", renewed, took)
	}

	// The renewals stopped reaching the server and the laptop took it over.
	setLease(t, tracker, "lampas", application.CrossSlotLease{
		Holder: "builder@laptop closing out mw-x.3", Host: "laptop", Token: "laptop-3", Taken: k.Now(), Expires: k.Now().Add(slotTTL),
	})
	k.beat <- struct{}{}
	trouble := held.Release(context.Background())
	if len(trouble) != 1 || !strings.Contains(trouble[0], "was lost while this landing held it") || !strings.Contains(trouble[0], "builder@laptop closing out mw-x.3") {
		t.Fatalf("expected the loss said, got %q", trouble)
	}
	if lease, _ := leaseOf(t, tracker, "lampas"); lease.Token != "laptop-3" {
		t.Fatalf("expected the laptop's lease left alone, got %+v", lease)
	}
}

func TestACloseOutGoesAheadWithoutTheCrossSlotAfterTheCap(t *testing.T) {
	tracker, k := apptest.NewFakeTracker(), newSlotClock()
	setLease(t, tracker, "lampas", application.CrossSlotLease{
		Holder: "builder@laptop closing out mw-x.4", Host: "laptop", Token: "laptop-4", Taken: k.Now(), Expires: k.Now().Add(slotTTL),
	})
	k.paused = func(time.Duration) {
		lease, _ := leaseOf(t, tracker, "lampas")
		lease.Expires = k.Now().Add(slotTTL)
		setLease(t, tracker, "lampas", lease)
	}
	slot := testSlot(tracker, k)
	slot.Cap = 30 * time.Minute
	held, said := slot.Take(context.Background(), "lampas", "me")
	if held != nil {
		t.Fatal("expected no holding")
	}
	want := "waited 30m for laptop's landing of lampas and went ahead without the cross-host merge slot: builder@laptop closing out mw-x.4 still held it, past the 30m a close-out waits for it"
	if len(said) != 1 || said[0] != want {
		t.Fatalf("expected %q, got %q", want, said)
	}
}

func TestTheZeroCrossSlotTakesNothingAndSaysNothing(t *testing.T) {
	held, said := application.CrossSlot{}.Take(context.Background(), "lampas", "me")
	if held != nil || said != nil {
		t.Fatalf("expected nothing, got %v %q", held, said)
	}
}

func TestTheReportSaysWhatBecameOfTheCrossSlot(t *testing.T) {
	report := application.NextReport{StoryID: "mw-y3qno5.3", Host: "desktop", CrossSlot: []string{"waited 27m for laptop's landing of lampas"}}
	if !strings.Contains(report.String(), "  slot    waited 27m for laptop's landing of lampas\n") {
		t.Fatalf("expected the wait on the report, got:\n%s", report.String())
	}
}

// flakyNotes fails its first reads, and then goes on to the tracker.
type flakyNotes struct {
	*apptest.FakeTracker
	failReads int // reads still to fail; negative fails them all
	err       error
	reads     int
}

func (f *flakyNotes) Note(ctx context.Context, key string) (string, error) {
	f.reads++
	if f.failReads != 0 {
		if f.failReads > 0 {
			f.failReads--
		}
		return "", f.err
	}
	return f.FakeTracker.Note(ctx, key)
}

func TestACrossSlotReadThatFailsTwiceIsTriedAgainAndTheSlotTaken(t *testing.T) {
	notes, k := &flakyNotes{FakeTracker: apptest.NewFakeTracker(), failReads: 2, err: errors.New("Dolt server unreachable at 10.88.0.2:3307")}, newSlotClock()
	held, said := testSlot(notes, k).Take(context.Background(), "lampas", "me")
	if held == nil || len(said) != 0 {
		t.Fatalf("expected the slot taken with nothing to say, got %v %q", held, said)
	}
	defer held.Release(context.Background())
	if lease, ok := leaseOf(t, notes.FakeTracker, "lampas"); !ok || lease.Host != "desktop" {
		t.Fatalf("expected the lease now this host's, got %+v", lease)
	}
}

func TestACrossSlotReadThatFailsPastTheRetryWindowIsSkippedSayingHowManyTriesAndForHowLong(t *testing.T) {
	notes, k := &flakyNotes{FakeTracker: apptest.NewFakeTracker(), failReads: -1, err: errors.New("Dolt server unreachable at 10.88.0.2:3307")}, newSlotClock()
	held, said := testSlot(notes, k).Take(context.Background(), "lampas", "me")
	if held != nil {
		t.Fatal("expected no holding")
	}
	// A poll of a minute, a window of two: tries at 0, 1 and 2 minutes.
	want := "the cross-host merge slot of lampas was skipped, so this landing went ahead under this host's merge slot alone: the beads server could not be reached: Dolt server unreachable at 10.88.0.2:3307 (3 tries failed over 2m)"
	if len(said) != 1 || said[0] != want {
		t.Fatalf("expected %q, got %q", want, said)
	}
	if notes.reads != 3 {
		t.Fatalf("expected 3 reads, got %d", notes.reads)
	}
}

func TestACrossSlotRetryCutShortByTheContextReturnsAtOnce(t *testing.T) {
	notes, k := &flakyNotes{FakeTracker: apptest.NewFakeTracker(), failReads: -1, err: errors.New("Dolt server unreachable")}, newSlotClock()
	ctx, cancel := context.WithCancel(context.Background())
	slot := testSlot(notes, k)
	slot.Wait = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	held, said := slot.Take(ctx, "lampas", "me")
	if held != nil {
		t.Fatal("expected no holding")
	}
	if len(said) != 1 || !strings.Contains(said[0], "was not taken") || !strings.Contains(said[0], "context canceled") || notes.reads != 1 {
		t.Fatalf("expected the slot not taken at once, got %d reads and %q", notes.reads, said)
	}
}

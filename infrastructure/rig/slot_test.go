package rig_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aRigDir is a directory standing in for a rig's checkout. The merge slot never
// looks inside it: it lives beside it, where the worktrees go.
func aRigDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making a rig directory: %v", err)
	}
	return dir
}

// impatient is a slots that waits barely at all, so that a test of contention
// takes milliseconds rather than minutes.
func impatient() *rig.Slots {
	return rig.NewSlots(rig.WithSlotWait(150*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond))
}

func TestOnlyOneCloseOutHoldsARigsMergeSlot(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)

	first, err := impatient().Take(ctx, dir, "builder@vps closing out mw-gq6.8")
	if err != nil {
		t.Fatalf("taking a free merge slot: %v", err)
	}

	if _, err := impatient().Take(ctx, dir, "builder@vps closing out mw-gq6.9"); err == nil {
		t.Fatal("expected a second close-out not to get the slot while the first has it")
	} else if !strings.Contains(err.Error(), "mw-gq6.8") {
		t.Errorf("expected the failure to name who has it, got %q", err)
	}

	if err := first.Release(ctx); err != nil {
		t.Fatalf("giving the slot back: %v", err)
	}
	second, err := impatient().Take(ctx, dir, "builder@vps closing out mw-gq6.9")
	if err != nil {
		t.Fatalf("expected the slot to be free once it was given back: %v", err)
	}
	if err := second.Release(ctx); err != nil {
		t.Errorf("giving the slot back again: %v", err)
	}
	// Releasing twice is how a close-out that already gave the slot back on its
	// way out of a failure is allowed to run its deferred release too.
	if err := second.Release(ctx); err != nil {
		t.Errorf("expected releasing twice to be harmless, got %v", err)
	}
}

func TestAMergeSlotNamesWhoeverHasItAndForgetsThemAfterwards(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)

	held, err := impatient().Take(ctx, dir, "builder@vps closing out mw-gq6.8")
	if err != nil {
		t.Fatalf("taking the slot: %v", err)
	}
	if held.HeldBy() != "builder@vps closing out mw-gq6.8" {
		t.Errorf("expected the holding to say who has it, got %q", held.HeldBy())
	}

	said, err := os.ReadFile(rig.SlotPath(dir))
	if err != nil {
		t.Fatalf("reading the slot: %v", err)
	}
	if !strings.Contains(string(said), "mw-gq6.8") || !strings.Contains(string(said), "pid ") {
		t.Errorf("expected the slot to say who has it and which process, got %q", said)
	}

	if err := held.Release(ctx); err != nil {
		t.Fatalf("giving the slot back: %v", err)
	}
	if said, err := os.ReadFile(rig.SlotPath(dir)); err != nil || strings.TrimSpace(string(said)) != "" {
		t.Errorf("expected a slot nobody has to name nobody, got %q (%v)", said, err)
	}
}

func TestWaitingForAMergeSlotGivesUpWhenTheContextDoes(t *testing.T) {
	dir := aRigDir(t)
	held, err := impatient().Take(context.Background(), dir, "somebody else")
	if err != nil {
		t.Fatalf("taking the slot: %v", err)
	}
	defer held.Release(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// A long wait, cut short by the context rather than by the wait itself.
	if _, err := rig.NewSlots(rig.WithSlotPoll(5*time.Millisecond)).Take(ctx, dir, "me"); err == nil {
		t.Fatal("expected waiting for the slot to give up when the context did")
	}
}

// slotHeldByHand is somebody holding a rig's merge slot through a lock of their
// own, so that a test can change who the slot says has it without ever letting
// the lock go: a hand-over with no gap for the waiter to slip through.
type slotHeldByHand struct {
	t    *testing.T
	file *os.File
}

func holdSlotByHand(t *testing.T, dir, holder string) *slotHeldByHand {
	t.Helper()
	path := rig.SlotPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("making the slot's directory: %v", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("opening the slot: %v", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("locking the slot: %v", err)
	}
	h := &slotHeldByHand{t: t, file: file}
	h.says(holder)
	return h
}

// says changes who the slot names, keeping the lock.
func (h *slotHeldByHand) says(holder string) {
	h.t.Helper()
	if err := h.file.Truncate(0); err != nil {
		h.t.Fatalf("emptying the slot: %v", err)
	}
	if _, err := h.file.WriteAt([]byte(holder+"\npid 1\ntaken "+time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0); err != nil {
		h.t.Fatalf("writing the slot: %v", err)
	}
}

func (h *slotHeldByHand) release() {
	syscall.Flock(int(h.file.Fd()), syscall.LOCK_UN)
	h.file.Close()
}

// anEpoch is where a test's wound clock starts.
var anEpoch = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// handOverEvery makes the slot change hands on the clock's own ticks, not on a
// timer of the test's: at every nth look the waiter takes at the slot, the next of
// holders has it, and once the last has had it for n looks the slot is let go. A
// waiter sees the same hand-offs however busy the host is, which a goroutine
// sleeping beside the waiter cannot promise (mw-gq6.423).
func handOverEvery(clock *aClock, h *slotHeldByHand, holders []string, n int) {
	ticks, gone := 0, false
	clock.onTick = func(time.Time) {
		ticks++
		if ticks%n != 0 || gone {
			return
		}
		if i := ticks/n - 1; i < len(holders) {
			h.says(holders[i])
			return
		}
		gone = true
		h.release()
	}
}

func TestAMergeSlotThatKeepsChangingHandsIsNotGivenUpOn(t *testing.T) {
	dir := aRigDir(t)
	clock := &aClock{at: anEpoch}
	slots := rig.NewSlots(rig.WithSlotWait(150*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond),
		rig.WithSlotClock(clock.now, clock.sleep))

	holders := []string{"closing out mw-1", "closing out mw-2", "closing out mw-3", "closing out mw-4", "closing out mw-5"}
	first := holdSlotByHand(t, dir, "closing out mw-0")
	defer first.release()
	// Each holder has it for six looks of 10ms: 60ms, well inside the 150ms wait.
	handOverEvery(clock, first, holders, 6)

	held, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err != nil {
		t.Fatalf("expected a waiter to keep waiting while the slot changes hands, got %v", err)
	}
	defer held.Release(context.Background())
	// Six holders of 60ms each is well past the 150ms wait: the wait was per holder.
	if waited := clock.at.Sub(anEpoch); waited < 150*time.Millisecond {
		t.Errorf("expected the waiter to have waited longer than the wait in all, waited %s", waited)
	}
}

func TestAMergeSlotOneHolderHasHeldForTheWholeWaitIsGivenUpOn(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-stuck")
	defer held.release()

	clock := &aClock{at: anEpoch}
	slots := rig.NewSlots(rig.WithSlotWait(100*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond),
		rig.WithSlotClock(clock.now, clock.sleep))
	_, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err == nil {
		t.Fatal("expected a waiter to give up on a slot one holder keeps for longer than the wait")
	}
	if !strings.Contains(err.Error(), "closing out mw-stuck") {
		t.Errorf("expected the failure to name the holder, got %q", err)
	}
	if !strings.Contains(err.Error(), "one holder") {
		t.Errorf("expected the failure to say it was one holder that kept it, got %q", err)
	}
	if waited := clock.at.Sub(anEpoch); waited != 100*time.Millisecond {
		t.Errorf("expected the waiter to give up after exactly the wait, waited %s", waited)
	}
}

func TestTheOverallCapStopsAWaiterWhoseSlotKeepsMoving(t *testing.T) {
	dir := aRigDir(t)
	clock := &aClock{at: anEpoch}
	slots := rig.NewSlots(rig.WithSlotWait(100*time.Millisecond), rig.WithSlotCap(250*time.Millisecond),
		rig.WithSlotPoll(10*time.Millisecond), rig.WithSlotClock(clock.now, clock.sleep))

	var holders []string
	for i := 0; i < 100; i++ {
		holders = append(holders, "closing out mw-"+strconv.Itoa(i))
	}
	first := holdSlotByHand(t, dir, "closing out mw-first")
	defer first.release()
	// Every holder has it for 30ms, so the slot is moving the whole 250ms.
	handOverEvery(clock, first, holders, 3)

	_, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err == nil {
		t.Fatal("expected the overall cap to stop a waiter whose slot keeps changing hands")
	}
	if !strings.Contains(err.Error(), "overall") {
		t.Errorf("expected the failure to say it was the overall cap, got %q", err)
	}
	if waited := clock.at.Sub(anEpoch); waited != 250*time.Millisecond {
		t.Errorf("expected the waiter to stop at exactly the cap, waited %s", waited)
	}
}

func TestACancelledContextStopsAWaiterAtOnce(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-busy")
	defer held.release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	slots := rig.NewSlots(rig.WithSlotWait(time.Hour), rig.WithSlotPoll(time.Hour))
	started := time.Now()
	_, err := slots.Take(ctx, dir, "closing out mw-me")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a cancelled context to be why the waiter gave up, got %v", err)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("expected the waiter to give up at once, waited %s", waited)
	}
}

func TestWaitingForAHeldSlotSaysWhoHasIt(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)

	first, err := impatient().Take(ctx, dir, "builder@laptop closing out mw-gq6.8")
	if err != nil {
		t.Fatalf("taking a free merge slot: %v", err)
	}
	defer first.Release(ctx)

	var said []string
	waiting := rig.NewSlots(rig.WithSlotWait(100*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond),
		rig.WithSlotNotice(func(s string) { said = append(said, s) }))
	if _, err := waiting.Take(ctx, dir, "builder@laptop checking mw-gq6.9"); err == nil {
		t.Fatal("expected the wait to give up while the slot is held")
	}
	if len(said) != 1 || !strings.Contains(said[0], "waiting for the merge slot: held by builder@laptop closing out mw-gq6.8") {
		t.Errorf("expected one notice naming the holder, got %q", said)
	}
}

// aClock is a clock a test winds by hand: sleeping is moving it on, so a wait of
// half an hour takes no time, and a hook runs at each look at the slot.
type aClock struct {
	at     time.Time
	onTick func(at time.Time)
}

func (c *aClock) now() time.Time { return c.at }

func (c *aClock) sleep(_ context.Context, d time.Duration) error {
	c.at = c.at.Add(d)
	if c.onTick != nil {
		c.onTick(c.at)
	}
	return nil
}

func TestACloseOutWaitsOutAHolderForAsLongAsTheRigsGateTakes(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-landing")
	clock := &aClock{at: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	release := clock.at.Add(25 * time.Minute)
	clock.onTick = func(at time.Time) {
		if !at.Before(release) {
			held.release()
		}
	}
	var asked string
	slots := rig.NewSlots(
		rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep),
		rig.WithSlotWaitFor(func(_ context.Context, rigDir string) time.Duration {
			asked = rigDir
			return 58 * time.Minute
		}),
	)

	taken, err := slots.Take(context.Background(), dir, "closing out mw-next")
	if err != nil {
		t.Fatalf("expected a close-out to wait 25 minutes for a holder when the rig's gate takes that long, got %v", err)
	}
	defer taken.Release(context.Background())
	if asked != dir {
		t.Errorf("expected the wait to be worked out for the rig %q, was asked about %q", dir, asked)
	}
}

func TestAHolderPastTheScaledWaitStopsTheCloseOutAndTheMessageNamesTheWait(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-stuck")
	defer held.release()
	clock := &aClock{at: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	slots := rig.NewSlots(
		rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep),
		rig.WithSlotWaitFor(func(context.Context, string) time.Duration { return 58 * time.Minute }),
	)

	_, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err == nil {
		t.Fatal("expected a holder that outlasts the scaled wait to stop the close-out")
	}
	if !strings.Contains(err.Error(), "closing out mw-stuck") || !strings.Contains(err.Error(), "58m0s") {
		t.Errorf("expected the failure to name the holder and the wait used, got %q", err)
	}
}

func TestARigWithNoGateHistoryWaitsTheDefaultPerHolder(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-stuck")
	defer held.release()
	clock := &aClock{at: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	start := clock.at
	slots := rig.NewSlots(
		rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep),
		rig.WithSlotWaitFor(func(context.Context, string) time.Duration { return 0 }),
	)

	_, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err == nil {
		t.Fatal("expected a stuck holder to stop the close-out")
	}
	if waited := clock.at.Sub(start); waited != rig.SlotWait {
		t.Errorf("expected a wait of %s with no gate history, waited %s", rig.SlotWait, waited)
	}
	if !strings.Contains(err.Error(), "20m0s") {
		t.Errorf("expected the failure to name the wait used, got %q", err)
	}
}

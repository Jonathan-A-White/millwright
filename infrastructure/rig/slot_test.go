package rig_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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
// takes milliseconds rather than minutes. Its holder in such a test is this
// very process, alive, so it is the cap that ends the wait (mw-gq6.467).
func impatient() *rig.Slots {
	return rig.NewSlots(rig.WithSlotWait(150*time.Millisecond), rig.WithSlotCap(150*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond))
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
// the lock go: a hand-over with no gap for the waiter to slip through. The slot
// names pid as the holder's process.
type slotHeldByHand struct {
	t    *testing.T
	file *os.File
	pid  int
}

// holdSlotByHand holds the slot for holder, naming a process that is gone: a
// stuck holder, the one the per-holder wait is for (mw-gq6.467).
func holdSlotByHand(t *testing.T, dir, holder string) *slotHeldByHand {
	t.Helper()
	return holdSlotByHandAs(t, dir, holder, gonePid(t))
}

// gonePid is the process id of a process that has ended.
func gonePid(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("running a process to end: %v", err)
	}
	return cmd.Process.Pid
}

func holdSlotByHandAs(t *testing.T, dir, holder string, pid int) *slotHeldByHand {
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
	h := &slotHeldByHand{t: t, file: file, pid: pid}
	h.says(holder)
	return h
}

// says changes who the slot names, keeping the lock.
func (h *slotHeldByHand) says(holder string) {
	h.t.Helper()
	if err := h.file.Truncate(0); err != nil {
		h.t.Fatalf("emptying the slot: %v", err)
	}
	if _, err := h.file.WriteAt([]byte(holder+"\npid "+strconv.Itoa(h.pid)+"\ntaken "+time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0); err != nil {
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
	if !strings.Contains(err.Error(), "overall") || !errors.Is(err, application.ErrSlotCap) {
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
	waiting := rig.NewSlots(rig.WithSlotWait(100*time.Millisecond), rig.WithSlotCap(100*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond),
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

// A holder whose process is alive is still at its close-out, however long its
// tests take on a loaded host: it is waited on past the per-holder wait, up to
// the cap (mw-gq6.467, where mw-emk3xt.1 gave up after 52m on a live holder).
func TestALiveHolderIsWaitedOnPastThePerHolderWait(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHandAs(t, dir, "builder@laptop closing out mw-5r3p30.318", os.Getpid())
	clock := &aClock{at: anEpoch}
	release := anEpoch.Add(52 * time.Minute)
	clock.onTick = func(at time.Time) {
		if !at.Before(release) {
			held.release()
		}
	}
	slots := rig.NewSlots(rig.WithSlotWait(20*time.Minute), rig.WithSlotCap(2*time.Hour), rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep))

	taken, err := slots.Take(context.Background(), dir, "builder@laptop closing out mw-emk3xt.1")
	if err != nil {
		t.Fatalf("expected a live holder to be waited on past the per-holder wait, got %v", err)
	}
	defer taken.Release(context.Background())
	if waited := clock.at.Sub(anEpoch); waited != 52*time.Minute {
		t.Errorf("expected the slot to be taken when its holder let go, after 52m, waited %s", waited)
	}
}

func TestALiveHolderThatNeverLetsGoStopsTheWaiterAtTheCap(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHandAs(t, dir, "builder@laptop closing out mw-5r3p30.318", os.Getpid())
	defer held.release()
	clock := &aClock{at: anEpoch}
	slots := rig.NewSlots(rig.WithSlotWait(20*time.Minute), rig.WithSlotCap(2*time.Hour), rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep))

	_, err := slots.Take(context.Background(), dir, "builder@laptop closing out mw-emk3xt.1")
	if !errors.Is(err, application.ErrSlotCap) {
		t.Fatalf("expected the waiter to stop at the overall cap, got %v", err)
	}
	if waited := clock.at.Sub(anEpoch); waited != 2*time.Hour {
		t.Errorf("expected the waiter to stop at exactly the cap, waited %s", waited)
	}
}

// takeSlotInTheBackground starts a Take for holder, polling every poll, and
// returns once it has found the slot held, so that waiters started one after
// another begin waiting in that order (mw-gq6.586). Each winner is noted in
// order, and gives the slot back at once.
func takeSlotInTheBackground(t *testing.T, dir, holder string, poll time.Duration, order *[]string, mu *sync.Mutex, done chan<- error) {
	t.Helper()
	waiting := make(chan struct{})
	var once sync.Once
	slots := rig.NewSlots(rig.WithSlotPoll(poll),
		rig.WithSlotNotice(func(string) { once.Do(func() { close(waiting) }) }))
	go func() {
		held, err := slots.Take(context.Background(), dir, holder)
		if err == nil {
			mu.Lock()
			*order = append(*order, holder)
			mu.Unlock()
			err = held.Release(context.Background())
		}
		done <- err
	}()
	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never found the merge slot held", holder)
	}
}

// A rig's merge slot is first come, first served: close-outs take it in the order
// they began waiting, however their polls fall. The earliest polls slowest here,
// so a slot that went to whoever polled first would give it to the last (mw-gq6.586).
func TestWaitersTakeARigsMergeSlotInTheOrderTheyBeganWaiting(t *testing.T) {
	for round := 0; round < 3; round++ {
		dir := aRigDir(t)
		first, err := rig.NewSlots().Take(context.Background(), dir, "closing out mw-0")
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		var mu sync.Mutex
		done := make(chan error, 3)
		takeSlotInTheBackground(t, dir, "closing out mw-a", 60*time.Millisecond, &order, &mu, done)
		takeSlotInTheBackground(t, dir, "closing out mw-b", 25*time.Millisecond, &order, &mu, done)
		takeSlotInTheBackground(t, dir, "closing out mw-c", 2*time.Millisecond, &order, &mu, done)
		if err := first.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("a waiter never took the merge slot")
			}
		}
		mu.Lock()
		got := strings.Join(order, ",")
		mu.Unlock()
		if want := "closing out mw-a,closing out mw-b,closing out mw-c"; got != want {
			t.Fatalf("round %d: expected the waiters to take the slot in the order a, b, c, got %s", round, got)
		}
	}
}

// slotTicket leaves a ticket in a rig's merge slot queue, as a waiter that began
// its wait at began would.
func slotTicket(t *testing.T, dir string, began time.Time, holder string) string {
	t.Helper()
	queue := rig.SlotPath(dir) + rig.SlotQueueSuffix
	if err := os.MkdirAll(queue, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(queue, fmt.Sprintf("%020d-%010d-0", began.UnixNano(), 1))
	if err := os.WriteFile(path, []byte(holder+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A waiter whose process is gone leaves a ticket nobody holds; it is skipped and
// cleared, so that it never blocks the queue (mw-gq6.586).
func TestADeadWaitersTicketDoesNotBlockTheMergeSlot(t *testing.T) {
	dir := aRigDir(t)
	ticket := slotTicket(t, dir, time.Now().Add(-time.Minute), "closing out mw-dead")
	held, err := rig.NewSlots(rig.WithSlotPoll(2*time.Millisecond), rig.WithSlotCap(5*time.Second)).Take(context.Background(), dir, "closing out mw-b")
	if err != nil {
		t.Fatalf("expected a dead waiter not to block the queue, got %v", err)
	}
	defer held.Release(context.Background())
	if _, err := os.Stat(ticket); !os.IsNotExist(err) {
		t.Errorf("expected the dead waiter's ticket cleared, stat gave %v", err)
	}
}

// A ticket older than the cap is skipped even while its owner is alive: its
// owner has given up by then.
func TestATicketOlderThanTheCapDoesNotBlockTheMergeSlot(t *testing.T) {
	dir := aRigDir(t)
	ticket := slotTicket(t, dir, time.Now().Add(-time.Hour), "closing out mw-old")
	file, err := os.Open(ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	held, err := rig.NewSlots(rig.WithSlotPoll(2*time.Millisecond), rig.WithSlotCap(time.Minute)).Take(context.Background(), dir, "closing out mw-b")
	if err != nil {
		t.Fatalf("expected an over-age ticket not to block the queue, got %v", err)
	}
	held.Release(context.Background())
}

// A waiter behind a live waiter is told who it waits for, though the slot itself
// is free for a moment: the head of the queue has not taken it yet.
func TestAWaiterBehindALiveWaiterForTheMergeSlotNamesTheHeadOfTheQueue(t *testing.T) {
	dir := aRigDir(t)
	ticket := slotTicket(t, dir, time.Now(), "closing out mw-head")
	file, err := os.Open(ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var said []string
	_, err = rig.NewSlots(rig.WithSlotPoll(time.Minute), rig.WithSlotNotice(func(s string) { said = append(said, s); cancel() })).Take(ctx, dir, "closing out mw-b")
	if err == nil || len(said) != 1 || !strings.Contains(said[0], "held by closing out mw-head") {
		t.Fatalf("expected the wait to name the head of the queue, got %v and %q", err, said)
	}
	if entries, _ := os.ReadDir(rig.SlotPath(dir) + rig.SlotQueueSuffix); len(entries) != 1 {
		t.Errorf("expected only the head's ticket left, got %d entries", len(entries))
	}
}

func TestWorktreeChecksOfOneRigShareTheMergeSlot(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)

	first, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1")
	if err != nil {
		t.Fatalf("taking a free merge slot shared: %v", err)
	}
	second, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-b.2")
	if err != nil {
		t.Fatalf("expected a second check to share the slot with the first, got %v", err)
	}

	// A landing takes the slot alone: it waits for both checks and names them.
	_, err = impatient().Take(ctx, dir, "mw next landing mw-c.3")
	if err == nil {
		t.Fatal("expected a landing not to get the slot while checks hold it")
	}
	for _, who := range []string{"mw-a.1", "mw-b.2"} {
		if !strings.Contains(err.Error(), who) {
			t.Errorf("expected the failure to name the check of %s, got %q", who, err)
		}
	}

	if err := first.Release(ctx); err != nil {
		t.Fatalf("giving the first back: %v", err)
	}
	if _, err := impatient().Take(ctx, dir, "mw next landing mw-c.3"); err == nil {
		t.Fatal("expected a landing to wait for the check still running")
	}
	if err := second.Release(ctx); err != nil {
		t.Fatalf("giving the second back: %v", err)
	}
	landing, err := impatient().Take(ctx, dir, "mw next landing mw-c.3")
	if err != nil {
		t.Fatalf("expected the landing to get the slot once the checks were done: %v", err)
	}

	// And a check waits for the landing.
	if _, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1"); err == nil {
		t.Fatal("expected a check to wait for a landing")
	} else if !strings.Contains(err.Error(), "mw-c.3") {
		t.Errorf("expected the failure to name the landing, got %q", err)
	}
	if err := landing.Release(ctx); err != nil {
		t.Fatalf("giving the landing back: %v", err)
	}
	if shared, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1"); err != nil {
		t.Errorf("expected a check to get the slot after the landing: %v", err)
	} else {
		shared.Release(ctx)
	}
}

func TestAWaitingLandingIsNotStarvedByChecksArrivingBehindIt(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)

	running, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1")
	if err != nil {
		t.Fatalf("taking the slot shared: %v", err)
	}
	defer running.Release(ctx)

	landing := make(chan error, 1)
	go func() {
		slots := rig.NewSlots(rig.WithSlotCap(5*time.Second), rig.WithSlotPoll(10*time.Millisecond))
		held, err := slots.Take(ctx, dir, "mw next landing mw-c.3")
		if err == nil {
			held.Release(ctx)
		}
		landing <- err
	}()
	time.Sleep(100 * time.Millisecond)

	// A check arriving after the landing queued waits behind it.
	patient := rig.NewSlots(rig.WithSlotPoll(10 * time.Millisecond))
	brief, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if _, err := patient.TakeShared(brief, dir, "builder@laptop checking mw-b.2"); err == nil {
		t.Fatal("expected a check that came after the landing to wait for its turn")
	}
	running.Release(ctx)
	if err := <-landing; err != nil {
		t.Fatalf("expected the landing to get its turn when the running check ended: %v", err)
	}
}

func TestAWaitForTheSlotThatEndsWithTheContextNamesWhoHasItAndSince(t *testing.T) {
	dir := aRigDir(t)

	held, err := impatient().TakeShared(context.Background(), dir, "builder@laptop checking mw-a.1")
	if err != nil {
		t.Fatalf("taking the slot: %v", err)
	}
	defer held.Release(context.Background())
	landing, err := impatient().Take(context.Background(), dir, "x")
	if err == nil {
		landing.Release(context.Background())
		t.Fatal("expected the exclusive take to wait")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	slots := rig.NewSlots(rig.WithSlotPoll(10 * time.Millisecond))
	_, err = slots.Take(ctx, dir, "mw next landing mw-c.3")
	var waited *application.SlotWaitError
	if !errors.As(err, &waited) {
		t.Fatalf("expected a SlotWaitError, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected the wait to have ended with the context's deadline, got %v", err)
	}
	if waited.Holder != "builder@laptop checking mw-a.1" {
		t.Errorf("expected the holder to be named, got %q", waited.Holder)
	}
	if waited.Since.IsZero() || time.Since(waited.Since) > time.Minute {
		t.Errorf("expected the time the holder took the slot, got %v", waited.Since)
	}
}

func TestAShareHolderThatDiedLeavesNoRecordBehind(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)
	record := rig.SlotPath(dir) + rig.SlotSharedSuffix
	if err := os.MkdirAll(record, 0o755); err != nil {
		t.Fatal(err)
	}
	// A holder whose process is gone: the kernel gave its lock back.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	says := fmt.Sprintf("builder@laptop checking mw-gone\npid %d\ntaken %s\n", dead.Process.Pid, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(record, "gone"), []byte(says), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := impatient().Take(ctx, dir, "mw next landing mw-c.3"); err != nil {
		t.Fatalf("expected a dead check's record not to hold the slot: %v", err)
	}
}

// yielding takes the slot alone and asserts it can be yielded, as a landing's is.
func yielding(t *testing.T, dir string) application.YieldingHolding {
	t.Helper()
	held, err := rig.NewSlots(rig.WithSlotPoll(10*time.Millisecond)).Take(context.Background(), dir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatalf("taking the slot: %v", err)
	}
	y, ok := held.(application.YieldingHolding)
	if !ok {
		t.Fatal("expected a holding taken alone to be one that can be yielded")
	}
	return y
}

func TestALandingThatYieldsLetsAChecksPartShareTheSlotButNotALanding(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)
	landing := yielding(t, dir)
	defer landing.Release(ctx)

	if _, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1"); err == nil {
		t.Fatal("expected a check to wait while the landing holds the slot alone")
	}
	if err := landing.Yield(ctx); err != nil {
		t.Fatalf("yielding: %v", err)
	}

	check, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1")
	if err != nil {
		t.Fatalf("expected a check to start while the landing waits for its fixer: %v", err)
	}
	defer check.Release(ctx)
	// Another landing is still kept out: the batch holds the slot, shared.
	other, err := impatient().Take(ctx, dir, "mw next landing mw-c.3")
	if err == nil {
		other.Release(ctx)
		t.Fatal("expected another landing to wait while the batch waits for its fixer")
	}
	if !strings.Contains(err.Error(), "landing a batch of millwright") {
		t.Errorf("expected the wait to name the batch, got %q", err)
	}
}

func TestACheckQueuedBehindALandingStillStartsWhileTheBatchYields(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)
	landing := yielding(t, dir)
	defer landing.Release(ctx)
	if err := landing.Yield(ctx); err != nil {
		t.Fatalf("yielding: %v", err)
	}

	// A close-out waits for the slot, queued ahead of the check.
	go func() {
		slots := rig.NewSlots(rig.WithSlotCap(5*time.Second), rig.WithSlotPoll(10*time.Millisecond))
		if held, err := slots.Take(ctx, dir, "mw next landing mw-c.3"); err == nil {
			held.Release(ctx)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	brief, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	check, err := rig.NewSlots(rig.WithSlotPoll(10*time.Millisecond)).TakeShared(brief, dir, "builder@laptop checking mw-a.1")
	if err != nil {
		t.Fatalf("expected a check to start beside the yielding batch, though a close-out is queued: %v", err)
	}
	check.Release(ctx)
}

func TestALandingThatResumesWaitsForTheRunningCheckAndKeepsNewOnesOut(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)
	landing := yielding(t, dir)
	defer landing.Release(ctx)
	if err := landing.Yield(ctx); err != nil {
		t.Fatalf("yielding: %v", err)
	}
	check, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-a.1")
	if err != nil {
		t.Fatalf("taking the slot beside the batch: %v", err)
	}

	resumed := make(chan error, 1)
	go func() { resumed <- landing.Resume(ctx) }()
	select {
	case err := <-resumed:
		t.Fatalf("expected the batch to wait for the running check before its tests, got %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	// The batch is back at its tests: a check arriving now waits for it.
	brief, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if late, err := rig.NewSlots(rig.WithSlotPoll(10*time.Millisecond)).TakeShared(brief, dir, "builder@laptop checking mw-b.2"); err == nil {
		late.Release(ctx)
		t.Fatal("expected a check arriving as the batch resumes to wait for it")
	}

	check.Release(ctx)
	select {
	case err := <-resumed:
		if err != nil {
			t.Fatalf("resuming once the check ended: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the batch to resume once the check ended")
	}
	if _, err := impatient().TakeShared(ctx, dir, "builder@laptop checking mw-b.2"); err == nil {
		t.Fatal("expected a batch running its tests to keep a check out")
	}
	if err := landing.Release(ctx); err != nil {
		t.Fatalf("giving the slot back: %v", err)
	}
	if free, err := impatient().Take(ctx, dir, "mw next landing mw-c.3"); err != nil {
		t.Errorf("expected the slot to be free after the batch: %v", err)
	} else {
		free.Release(ctx)
	}
}

func TestASlotWaitSaysHowLongItHasWaitedWhileItGoesOn(t *testing.T) {
	ctx, dir := context.Background(), aRigDir(t)
	landing, err := impatient().Take(ctx, dir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	defer landing.Release(ctx)

	var said []string
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	sleep := func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		now = now.Add(d)
		n := len(said)
		mu.Unlock()
		if n >= 3 {
			return context.Canceled
		}
		return nil
	}
	slots := rig.NewSlots(rig.WithSlotClock(clock, sleep), rig.WithSlotPoll(30*time.Second), rig.WithSlotNoticeEvery(time.Minute),
		rig.WithSlotNotice(func(s string) { mu.Lock(); said = append(said, s); mu.Unlock() }))
	_, err = slots.TakeShared(ctx, dir, "builder@laptop checking mw-a.1")
	if err == nil {
		t.Fatal("expected the wait to end with the sleep's error")
	}
	if len(said) < 3 {
		t.Fatalf("expected the wait to say again how long it had waited, got %q", said)
	}
	if !strings.Contains(said[1], "landing a batch of millwright") || !strings.Contains(said[1], "1m") {
		t.Errorf("expected the later notice to name the holder and the wait so far, got %q", said[1])
	}
}

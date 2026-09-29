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

// passItOn hands the slot from holder to holder, each for the given time, and
// then lets it go, unless stop is closed first. It returns once it has let go.
func passItOn(h *slotHeldByHand, holders []string, each time.Duration, stop <-chan struct{}) {
	defer h.release()
	for _, holder := range holders {
		h.says(holder)
		select {
		case <-stop:
			return
		case <-time.After(each):
		}
	}
}

func TestAMergeSlotThatKeepsChangingHandsIsNotGivenUpOn(t *testing.T) {
	dir := aRigDir(t)
	slots := rig.NewSlots(rig.WithSlotWait(150*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond))

	holders := []string{"closing out mw-1", "closing out mw-2", "closing out mw-3", "closing out mw-4", "closing out mw-5"}
	first := holdSlotByHand(t, dir, "closing out mw-0")
	done := make(chan struct{})
	go func() {
		defer close(done)
		passItOn(first, holders, 60*time.Millisecond, nil)
	}()

	started := time.Now()
	held, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err != nil {
		t.Fatalf("expected a waiter to keep waiting while the slot changes hands, got %v", err)
	}
	defer held.Release(context.Background())
	<-done
	// Six holders of 60ms each is well past the 150ms wait: the wait was per holder.
	if waited := time.Since(started); waited < 150*time.Millisecond {
		t.Errorf("expected the waiter to have waited longer than the wait in all, waited %s", waited)
	}
}

func TestAMergeSlotOneHolderHasHeldForTheWholeWaitIsGivenUpOn(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-stuck")
	defer held.release()

	slots := rig.NewSlots(rig.WithSlotWait(100*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond))
	started := time.Now()
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
	if waited := time.Since(started); waited > 2*time.Second {
		t.Errorf("expected the waiter to give up after about the wait, waited %s", waited)
	}
}

func TestTheOverallCapStopsAWaiterWhoseSlotKeepsMoving(t *testing.T) {
	dir := aRigDir(t)
	slots := rig.NewSlots(rig.WithSlotWait(100*time.Millisecond), rig.WithSlotCap(250*time.Millisecond), rig.WithSlotPoll(10*time.Millisecond))

	var holders []string
	for i := 0; i < 100; i++ {
		holders = append(holders, "closing out mw-"+strconv.Itoa(i))
	}
	first := holdSlotByHand(t, dir, "closing out mw-first")
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		passItOn(first, holders, 30*time.Millisecond, stop)
	}()
	defer func() { close(stop); <-done }()

	started := time.Now()
	_, err := slots.Take(context.Background(), dir, "closing out mw-me")
	if err == nil {
		t.Fatal("expected the overall cap to stop a waiter whose slot keeps changing hands")
	}
	if !strings.Contains(err.Error(), "overall") {
		t.Errorf("expected the failure to say it was the overall cap, got %q", err)
	}
	if waited := time.Since(started); waited < 250*time.Millisecond || waited > 2*time.Second {
		t.Errorf("expected the waiter to stop at about the cap, waited %s", waited)
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

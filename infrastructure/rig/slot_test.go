package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	if _, err := rig.NewSlots(rig.WithSlotPoll(5 * time.Millisecond)).Take(ctx, dir, "me"); err == nil {
		t.Fatal("expected waiting for the slot to give up when the context did")
	}
}

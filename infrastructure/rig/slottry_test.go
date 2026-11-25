package rig

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTryTakeIsRefusedAtOnceWhileSomebodyHasTheSlotAndTakenWhenFree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "postern")
	slots := NewSlots(WithSlotSuffix(AfterLandingSlotSuffix))
	ctx := context.Background()

	first, err := slots.TryTake(ctx, dir, "a deploy")
	if err != nil {
		t.Fatalf("a free slot was not taken: %v", err)
	}
	_, err = slots.TryTake(ctx, dir, "a tick")
	var held *application.SlotHeldError
	if !errors.As(err, &held) {
		t.Fatalf("expected a held slot, got %v", err)
	}
	if held.Holder != "a deploy" || held.Since.IsZero() {
		t.Fatalf("expected the holder and when it took the slot, got %+v", held)
	}
	if err := first.Release(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := slots.TryTake(ctx, dir, "a tick")
	if err != nil {
		t.Fatalf("the freed slot was not taken: %v", err)
	}
	_ = again.Release(ctx)
}

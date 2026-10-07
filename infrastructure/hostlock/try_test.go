package hostlock_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// A lock taken without waiting: a second caller is told it is held, at once,
// and Held says so without taking it; once let go it is free again.
func TestTryTakesWithoutWaitingAndHeldLooksWithoutTaking(t *testing.T) {
	ctx, dir := context.Background(), filepath.Join(t.TempDir(), "grist")
	grind := hostlock.NewTry(dir, hostlock.GrindFile(0))

	if held, err := grind.Held(ctx); err != nil || held {
		t.Fatalf("expected a lock never taken not held, got %v %v", held, err)
	}
	release, taken, err := grind.TryTake(ctx)
	if err != nil || !taken {
		t.Fatalf("expected a free lock taken, got %v %v", taken, err)
	}
	if info, err := os.Stat(filepath.Join(dir, hostlock.GrindFile(0))); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the lock file 0600, got %v %v", info, err)
	}
	if held, err := hostlock.NewTry(dir, hostlock.GrindFile(0)).Held(ctx); err != nil || !held {
		t.Fatalf("expected the lock held, got %v %v", held, err)
	}

	started := time.Now()
	if again, taken, err := hostlock.NewTry(dir, hostlock.GrindFile(0)).TryTake(ctx); err != nil || taken || again != nil {
		t.Fatalf("expected a held lock not taken, got %v %v", taken, err)
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("expected no real wait for a held lock, waited %s", waited)
	}
	if _, taken, _ := hostlock.NewTry(dir, hostlock.PassFile).TryTake(ctx); !taken {
		t.Fatal("expected the pass lock apart from the grind lock")
	}

	release()
	if held, _ := grind.Held(ctx); held {
		t.Fatal("expected the lock free once let go")
	}
	if release, taken, _ := grind.TryTake(ctx); !taken {
		t.Fatal("expected the lock taken again once let go")
	} else {
		release()
	}
}

// A host's grind slots are locks of their own: holding one leaves the others
// free, and each is a file named for its number.
func TestGrindSlotsAreLocksOfTheirOwn(t *testing.T) {
	ctx, dir := context.Background(), filepath.Join(t.TempDir(), "grist")
	slots := hostlock.GrindSlots(dir, 2)
	if len(slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(slots))
	}
	release, taken, err := slots[0].TryTake(ctx)
	if err != nil || !taken {
		t.Fatalf("expected the first slot taken, got %v %v", taken, err)
	}
	defer release()
	if held, _ := slots[1].Held(ctx); held {
		t.Fatal("expected the second slot free while the first is held")
	}
	if _, err := os.Stat(filepath.Join(dir, "grind-0.lock")); err != nil {
		t.Fatalf("expected grind-0.lock: %v", err)
	}
	if got := len(hostlock.GrindSlots(dir, 0)); got != 1 {
		t.Fatalf("expected a concurrency below 1 to leave one slot, got %d", got)
	}
}

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
	grind := hostlock.NewTry(dir, hostlock.GrindFile)

	if held, err := grind.Held(ctx); err != nil || held {
		t.Fatalf("expected a lock never taken not held, got %v %v", held, err)
	}
	release, taken, err := grind.TryTake(ctx)
	if err != nil || !taken {
		t.Fatalf("expected a free lock taken, got %v %v", taken, err)
	}
	if info, err := os.Stat(filepath.Join(dir, hostlock.GrindFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected the lock file 0600, got %v %v", info, err)
	}
	if held, err := hostlock.NewTry(dir, hostlock.GrindFile).Held(ctx); err != nil || !held {
		t.Fatalf("expected the lock held, got %v %v", held, err)
	}

	started := time.Now()
	if again, taken, err := hostlock.NewTry(dir, hostlock.GrindFile).TryTake(ctx); err != nil || taken || again != nil {
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

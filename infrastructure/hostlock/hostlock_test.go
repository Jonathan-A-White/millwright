package hostlock_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// impatient is a lock that waits barely at all, so that a test of contention
// takes milliseconds rather than minutes.
func impatient(dir string) *hostlock.Lock {
	return hostlock.New(dir, hostlock.WithWait(150*time.Millisecond), hostlock.WithPoll(10*time.Millisecond))
}

func TestOnlyOneCallerHoldsTheLockAtOnce(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()

	release, err := impatient(dir).Take(ctx)
	if err != nil {
		t.Fatalf("taking a free lock: %v", err)
	}

	if _, err := impatient(dir).Take(ctx); err == nil {
		t.Fatal("expected a second caller not to get the lock while the first has it")
	} else if want := filepath.Join(dir, hostlock.File); !strings.Contains(err.Error(), want) {
		t.Errorf("expected the failure to name the lock file %q, got %q", want, err)
	}

	release()
	second, err := impatient(dir).Take(ctx)
	if err != nil {
		t.Fatalf("expected the lock to be free once it was released: %v", err)
	}
	second()
}

func TestTakeMakesTheDirectoryIfItIsMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "mw")
	release, err := impatient(dir).Take(context.Background())
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer release()
	if _, err := os.Stat(filepath.Join(dir, hostlock.File)); err != nil {
		t.Fatalf("expected the lock file to exist: %v", err)
	}
}

func TestWaitingForTheLockGivesUpWhenTheContextDoes(t *testing.T) {
	dir := t.TempDir()
	release, err := impatient(dir).Take(context.Background())
	if err != nil {
		t.Fatalf("taking the lock: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := hostlock.New(dir, hostlock.WithPoll(5*time.Millisecond)).Take(ctx); err == nil {
		t.Fatal("expected waiting for the lock to give up when the context did")
	}
}

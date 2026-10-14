package hostlock_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

func TestAFreeGateLockIsTakenAtOnceWithNoWait(t *testing.T) {
	gate := hostlock.NewGate(t.TempDir())
	turn, err := gate.Take(context.Background(), "mw-a", func(string) { t.Error("told of a holder when nobody held it") })
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Holding.Release(context.Background())
	if turn.For != "" || turn.Waited > time.Second {
		t.Errorf("expected no wait, got %+v", turn)
	}
	if turn.Holding.HeldBy() != "mw-a" {
		t.Errorf("expected the holding to name its holder, got %q", turn.Holding.HeldBy())
	}
}

func TestASecondGateWaitsForTheFirstAndNamesIt(t *testing.T) {
	dir := t.TempDir()
	first, err := hostlock.NewGate(dir).Take(context.Background(), "mw-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	var told []string
	seen := make(chan struct{})
	go func() {
		<-seen // released once the second has seen it held
		first.Holding.Release(context.Background())
	}()
	second, err := hostlock.NewGate(dir, hostlock.WithGatePoll(5*time.Millisecond)).
		Take(context.Background(), "mw-b", func(by string) { told = append(told, by); close(seen) })
	if err != nil {
		t.Fatal(err)
	}
	defer second.Holding.Release(context.Background())
	if second.For != "mw-a" || len(told) != 1 || told[0] != "mw-a" {
		t.Errorf("expected the second to wait for mw-a and be told once, got %+v and %v", second, told)
	}
	if second.Waited <= 0 {
		t.Errorf("expected a wait, got %+v", second)
	}
}

func TestAGateLockGivesUpAfterItsCap(t *testing.T) {
	dir := t.TempDir()
	first, err := hostlock.NewGate(dir).Take(context.Background(), "mw-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Holding.Release(context.Background())
	_, err = hostlock.NewGate(dir, hostlock.WithGatePoll(5*time.Millisecond), hostlock.WithGateCap(30*time.Millisecond)).
		Take(context.Background(), "mw-b", nil)
	if err == nil || !strings.Contains(err.Error(), "mw-a") {
		t.Fatalf("expected a refusal naming the holder, got %v", err)
	}
}

func TestAReleasedGateLockCanBeTakenAgainAndReleasingTwiceIsHarmless(t *testing.T) {
	dir := t.TempDir()
	first, _ := hostlock.NewGate(dir).Take(context.Background(), "mw-a", nil)
	if err := first.Holding.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := first.Holding.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, err := hostlock.NewGate(dir, hostlock.WithGateCap(50*time.Millisecond)).Take(context.Background(), "mw-b", nil)
	if err != nil || again.For != "" {
		t.Fatalf("expected the lock free, got %+v, %v", again, err)
	}
	again.Holding.Release(context.Background())
}

// A close-out holding its rig's merge slot across hosts asks for the gate lock
// without waiting, by giving up in held (mw-gq6.422): the lock must say it is
// held before it waits, and give up then and there.
func TestAGateLockAskedForWithoutWaitingIsRefusedAtOnceWhenHeld(t *testing.T) {
	dir := t.TempDir()
	first, err := hostlock.NewGate(dir).Take(context.Background(), "mw-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Holding.Release(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var heldBy string
	began := time.Now()
	_, err = hostlock.NewGate(dir, hostlock.WithGatePoll(time.Minute)).Take(ctx, "mw-b", func(by string) { heldBy = by; cancel() })
	if err == nil || heldBy != "mw-a" {
		t.Fatalf("expected the ask refused, told mw-a held it, got %v and %q", err, heldBy)
	}
	if waited := time.Since(began); waited > 5*time.Second {
		t.Fatalf("expected the ask refused at once, it waited %s", waited)
	}
}

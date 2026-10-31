package hostlock_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

// takeInTheBackground starts a Take for holder and returns once it has found the
// lock held, so that waiters started one after another begin waiting in that
// order (mw-gq6.585). The result arrives on the channel; each winner is noted in
// order, and gives the lock back at once.
func takeInTheBackground(t *testing.T, dir, holder string, order *[]string, mu *sync.Mutex, done chan<- error) {
	t.Helper()
	waiting := make(chan struct{})
	var once sync.Once
	go func() {
		turn, err := hostlock.NewGate(dir, hostlock.WithGatePoll(2*time.Millisecond)).
			Take(context.Background(), holder, func(string) { once.Do(func() { close(waiting) }) })
		if err == nil {
			mu.Lock()
			*order = append(*order, holder)
			mu.Unlock()
			err = turn.Holding.Release(context.Background())
		}
		done <- err
	}()
	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never found the gate lock held", holder)
	}
}

// The gate lock is first come, first served: waiters take it in the order they
// began waiting, however their polls fall (mw-gq6.585).
func TestWaitersTakeTheGateLockInTheOrderTheyBeganWaiting(t *testing.T) {
	for round := 0; round < 5; round++ {
		dir := t.TempDir()
		first, err := hostlock.NewGate(dir).Take(context.Background(), "mw-0", nil)
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		var mu sync.Mutex
		done := make(chan error, 3)
		for _, holder := range []string{"mw-a", "mw-b", "mw-c"} {
			takeInTheBackground(t, dir, holder, &order, &mu, done)
		}
		if err := first.Holding.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("a waiter never took the gate lock")
			}
		}
		if got := strings.Join(order, ","); got != "mw-a,mw-b,mw-c" {
			t.Fatalf("round %d: expected the waiters to take the lock in the order a, b, c, got %s", round, got)
		}
	}
}

func queueTicket(t *testing.T, dir, name, holder string) string {
	t.Helper()
	queue := filepath.Join(dir, hostlock.GateQueueDir)
	if err := os.MkdirAll(queue, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(queue, name)
	if err := os.WriteFile(path, []byte(holder+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A waiter whose process is gone leaves a ticket nobody holds; it is skipped and
// cleared, so that it never blocks the queue.
func TestADeadWaitersTicketIsSkipped(t *testing.T) {
	dir := t.TempDir()
	ticket := queueTicket(t, dir, fmt.Sprintf("%020d-%010d-0", time.Now().Add(-time.Minute).UnixNano(), 1), "mw-dead")
	turn, err := hostlock.NewGate(dir, hostlock.WithGatePoll(2*time.Millisecond), hostlock.WithGateCap(5*time.Second)).
		Take(context.Background(), "mw-b", nil)
	if err != nil {
		t.Fatalf("expected a dead waiter not to block the queue, got %v", err)
	}
	defer turn.Holding.Release(context.Background())
	if _, err := os.Stat(ticket); !os.IsNotExist(err) {
		t.Errorf("expected the dead waiter's ticket cleared, stat gave %v", err)
	}
}

// A ticket older than the cap is skipped even while its owner is alive: its
// owner has given up by then.
func TestATicketOlderThanTheCapIsSkipped(t *testing.T) {
	dir := t.TempDir()
	ticket := queueTicket(t, dir, fmt.Sprintf("%020d-%010d-0", time.Now().Add(-time.Hour).UnixNano(), 1), "mw-old")
	file, err := os.Open(ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	turn, err := hostlock.NewGate(dir, hostlock.WithGatePoll(2*time.Millisecond), hostlock.WithGateCap(time.Minute)).
		Take(context.Background(), "mw-b", nil)
	if err != nil {
		t.Fatalf("expected an over-age ticket not to block the queue, got %v", err)
	}
	turn.Holding.Release(context.Background())
}

// A waiter behind another waiter is told who it waits for, though the lock itself
// is free for a moment: the head of the queue has not taken it yet.
func TestAWaiterBehindALiveWaiterNamesTheHeadOfTheQueue(t *testing.T) {
	dir := t.TempDir()
	ticket := queueTicket(t, dir, fmt.Sprintf("%020d-%010d-0", time.Now().UnixNano(), 1), "mw-head")
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
	var heldBy string
	_, err = hostlock.NewGate(dir, hostlock.WithGatePoll(time.Minute)).Take(ctx, "mw-b", func(by string) { heldBy = by; cancel() })
	if err == nil || heldBy != "mw-head" {
		t.Fatalf("expected the ask refused behind mw-head, got %v and %q", err, heldBy)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, hostlock.GateQueueDir)); len(entries) != 1 {
		t.Errorf("expected only the head's ticket left, got %d entries", len(entries))
	}
}

func TestAGateLocksHolderIsNamedWhileItIsHeldAndNotAfter(t *testing.T) {
	gate := hostlock.NewGate(t.TempDir())
	if got := gate.Holder(); got != "" {
		t.Fatalf("holder of a lock nobody took = %q, want none", got)
	}
	turn, err := gate.Take(context.Background(), "batch 307 of lampas", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := gate.Holder(); got != "batch 307 of lampas" {
		t.Errorf("holder of a held lock = %q, want batch 307 of lampas", got)
	}
	turn.Holding.Release(context.Background())
	if got := gate.Holder(); got != "" {
		t.Errorf("holder of a released lock = %q, want none", got)
	}
}

func TestAGateLockWhoseHolderDiedLeavingItsNameHasNoHolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, hostlock.GateFile), []byte("mw-dead\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := hostlock.NewGate(dir).Holder(); got != "" {
		t.Errorf("holder of a lock only a name is left in = %q, want none", got)
	}
}

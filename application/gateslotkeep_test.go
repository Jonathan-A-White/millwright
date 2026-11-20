package application_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1319: a landing that gave its merge slot up for a wait gave the gate lock
// back and asked for it again at the end of every such wait, even when no check
// was queued for the lock, which could put it behind other gates queued
// meanwhile. The lock is given back only when somebody is waiting for it.

// countedGate is the host's gate lock that counts the times it was asked for.
type countedGate struct {
	inner application.GateLock
	mu    sync.Mutex
	takes int
}

func (g *countedGate) Take(ctx context.Context, holder string, held func(string)) (application.GateTurn, error) {
	g.mu.Lock()
	g.takes++
	g.mu.Unlock()
	return g.inner.Take(ctx, holder, held)
}

func (g *countedGate) asked() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.takes
}

func TestAYieldedWaitWithNoCheckKeepsTheGateLock(t *testing.T) {
	t.Run("a wait for a calm host gives it back all the same", func(t *testing.T) {
		l := newALandingAndACheckOfOneRig(t)
		defer l.holding.Release(context.Background())
		gate := &countedGate{inner: l.gate}

		land := l.land()
		land.Next.GateLock = gate
		land.Next.Load = &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}}
		land.Next.LoadWait = func(context.Context, time.Duration) error { return nil }

		got := land.GateTheBatch(context.Background(), l.rigDir, l.report())
		if !got.Passed {
			t.Fatalf("expected the landing's gate to pass, got %+v\n%s", got, l.said.String())
		}
		// No holder keeps the lock while it only waits for the host to calm, queued
		// for or not (mw-gq6.1420): it is given back for the wait and asked for again.
		if asked := gate.asked(); asked != 2 {
			t.Errorf("expected the gate lock to be given back for the wait for a calm host and asked for again after it, it was asked for %d times\n%s", asked, l.said.String())
		}
	})

	t.Run("a wait for the gate lock", func(t *testing.T) {
		l := newALandingAndACheckOfOneRig(t)
		defer l.holding.Release(context.Background())
		gate := &countedGate{inner: l.gate}
		other, err := l.gate.Take(context.Background(), "mw-gq6.894", nil)
		if err != nil {
			t.Fatal(err)
		}

		land := l.land()
		land.Next.GateLock = gate
		gated := make(chan application.Checked, 1)
		go func() { gated <- land.GateTheBatch(context.Background(), l.rigDir, l.report()) }()
		l.waitQueued(1)
		if err := other.Holding.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-gated:
			if !got.Passed {
				t.Fatalf("expected the landing's gate to pass, got %+v\n%s", got, l.said.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("expected the landing's gate to end\n%s", l.said.String())
		}
		if asked := gate.asked(); asked != 1 {
			t.Errorf("expected the gate lock to be asked for once, as no check waited behind it, it was asked for %d times\n%s", asked, l.said.String())
		}
	})
}

// mw-gq6.1351: a check that took the merge slot shared but has not yet queued for
// the gate lock is no waiter Waiting() counts, so a landing whose yielded wait
// ended in that moment kept the lock, and its taking the slot alone again waited
// for the check, which then waited for the lock (mw-gq6.1316). The lock is given
// back when somebody is queued for it or another holder shares the slot.

// sharerBox is the shared holding a test's check takes, set from another goroutine.
type sharerBox struct {
	mu sync.Mutex
	h  application.Holding
}

func (b *sharerBox) set(h application.Holding) { b.mu.Lock(); b.h = h; b.mu.Unlock() }
func (b *sharerBox) get() application.Holding  { b.mu.Lock(); defer b.mu.Unlock(); return b.h }

// sharedByOthers says whether another holder shares the slot l's landing yielded,
// as batchSlot does for Next.SlotShared.
func (l *aLandingAndACheckOfOneRig) sharedByOthers() bool {
	s, ok := l.holding.(application.SlotSharers)
	return ok && s.SharedByOthers()
}

// givesTheLockBackFor runs the landing's gate with a check that shares the slot and
// has queued no ticket for the gate lock, which is let go only once the lock is
// seen free: the lock must be given back for the landing to go on.
func (l *aLandingAndACheckOfOneRig) givesTheLockBackFor(t *testing.T, land application.BatchLand, sharer *sharerBox) (freed bool, gate application.Checked) {
	t.Helper()
	land.Next.SlotShared = l.sharedByOthers
	gated := make(chan application.Checked, 1)
	go func() { gated <- land.GateTheBatch(context.Background(), l.rigDir, l.report()) }()
	// Once the check shares the slot and the wait has ended, the landing waits for
	// the check to end before it takes the slot alone: the lock it holds then is the
	// one that stalls the check (mw-gq6.1316), and it must be free. Both are waited
	// for as events, with an outer bound that is not what is measured (mw-gq6.1482).
	for deadline := time.Now().Add(10 * time.Second); sharer.get() == nil; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("expected a check to share the slot\n%s", l.said.String())
		}
	}
	select {
	case <-l.resuming:
	case <-time.After(10 * time.Second):
		t.Fatalf("expected the landing to go to take the slot alone again\n%s", l.said.String())
	}
	freed = l.gate.Holder() == ""
	if err := sharer.get().Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case gate = <-gated:
	case <-time.After(5 * time.Second):
		t.Fatalf("expected the landing's gate to end\n%s", l.said.String())
	}
	return freed, gate
}

// slowGate is the host's gate lock whose holdings give the lock back only after a
// delay, as a busy host does (mw-gq6.1482): the test must wait for the lock to be
// given back, not for a time it hoped that would take.
type slowGate struct {
	inner application.GateLock
	delay time.Duration
}

func (g slowGate) Take(ctx context.Context, holder string, held func(string)) (application.GateTurn, error) {
	turn, err := g.inner.Take(ctx, holder, held)
	if turn.Holding != nil {
		turn.Holding = slowHolding{Holding: turn.Holding, delay: g.delay}
	}
	return turn, err
}

type slowHolding struct {
	application.Holding
	delay time.Duration
}

func (h slowHolding) Release(ctx context.Context) error {
	time.Sleep(h.delay)
	return h.Holding.Release(ctx)
}

// A landing that is slow to give the gate lock back, on a host under load, has
// still given it back when the check that shares the slot is let go of.
func TestASlowGiveBackOfTheGateLockIsWaitedFor(t *testing.T) {
	l := newALandingAndACheckOfOneRig(t)
	defer l.holding.Release(context.Background())
	other, err := l.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	var sharer sharerBox
	land := l.land()
	land.Next.GateLock = slowGate{inner: l.gate, delay: time.Second}
	go func() {
		l.waitQueued(1)
		s, err := l.slots.TakeShared(context.Background(), l.rigDir, "mw check mw-x.1")
		if err != nil {
			t.Error(err)
			return
		}
		sharer.set(s)
		other.Holding.Release(context.Background())
	}()
	freed, gate := l.givesTheLockBackFor(t, land, &sharer)
	if !freed {
		t.Errorf("expected the gate lock to be given back while a check shared the slot, it was kept\n%s", l.said.String())
	}
	if !gate.Passed {
		t.Errorf("expected the landing's gate to pass, got %+v\n%s", gate, l.said.String())
	}
}

func TestAYieldedWaitWithACheckSharingTheSlotGivesTheLockBack(t *testing.T) {
	t.Run("a wait for a calm host", func(t *testing.T) {
		l := newALandingAndACheckOfOneRig(t)
		defer l.holding.Release(context.Background())
		var sharer sharerBox
		land := l.land()
		land.Next.Load = &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}}
		land.Next.LoadWait = func(ctx context.Context, _ time.Duration) error {
			// A check takes the slot the landing gave up, and has queued no ticket for
			// the gate lock yet.
			h, err := l.slots.TakeShared(ctx, l.rigDir, "mw check mw-x.1")
			sharer.set(h)
			return err
		}
		freed, gate := l.givesTheLockBackFor(t, land, &sharer)
		if !freed {
			t.Errorf("expected the gate lock to be given back while a check shared the slot, it was kept\n%s", l.said.String())
		}
		if !gate.Passed {
			t.Errorf("expected the landing's gate to pass, got %+v\n%s", gate, l.said.String())
		}
	})

	t.Run("a wait for the gate lock", func(t *testing.T) {
		l := newALandingAndACheckOfOneRig(t)
		defer l.holding.Release(context.Background())
		other, err := l.gate.Take(context.Background(), "mw-gq6.894", nil)
		if err != nil {
			t.Fatal(err)
		}
		var sharer sharerBox
		land := l.land()
		go func() {
			l.waitQueued(1)
			// The landing waits for the lock with its slot shared; a check takes the
			// slot and queues for nothing, and then the lock is freed.
			s, err := l.slots.TakeShared(context.Background(), l.rigDir, "mw check mw-x.1")
			if err != nil {
				t.Error(err)
				return
			}
			sharer.set(s)
			other.Holding.Release(context.Background())
		}()
		freed, gate := l.givesTheLockBackFor(t, land, &sharer)
		if !freed {
			t.Errorf("expected the gate lock to be given back while a check shared the slot, it was kept\n%s", l.said.String())
		}
		if !gate.Passed {
			t.Errorf("expected the landing's gate to pass, got %+v\n%s", gate, l.said.String())
		}
	})
}

// mw-gq6.1457: a batch whose yielded wait for the gate lock ended while somebody
// was queued for the lock gave it back and asked again, whoever was queued, a check
// of another rig included, which cannot wait on the batch's merge slot. A batch
// (WithQueuePrecedence) that no other holder shares the slot with keeps the lock.

func TestABatchsSlotYieldKeepsTheGateLockFromAnotherRigsCheck(t *testing.T) {
	l := newALandingAndACheckOfOneRig(t)
	defer l.holding.Release(context.Background())
	gate := &countedGate{inner: l.gate}
	other, err := l.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}

	land := l.land()
	land.Next.GateLock = gate
	// Nobody shares the batch's slot: SlotShared says false, as batchSlot's does.
	land.Next.SlotShared = func() bool { return false }
	gated := make(chan application.Checked, 1)
	go func() {
		gated <- land.GateTheBatch(application.WithQueuePrecedence(context.Background()), l.rigDir, l.report())
	}()
	l.waitQueued(1)
	// A check of another rig queues for the gate lock behind the batch.
	go func() {
		turn, err := l.gate.Take(context.Background(), "mw check lampas-x.1", nil)
		if err == nil && turn.Holding != nil {
			turn.Holding.Release(context.Background())
		}
	}()
	l.waitQueued(2)
	if err := other.Holding.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-gated:
		if !got.Passed {
			t.Fatalf("expected the batch's gate to pass, got %+v\n%s", got, l.said.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("expected the batch's gate to end\n%s", l.said.String())
	}
	if asked := gate.asked(); asked != 1 {
		t.Errorf("expected the batch to keep the gate lock when only another rig's check was queued for it, it asked for the lock %d times\n%s", asked, l.said.String())
	}
}

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
	t.Run("a wait for a calm host", func(t *testing.T) {
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
		if asked := gate.asked(); asked != 1 {
			t.Errorf("expected the gate lock to be asked for once and kept through the wait for a calm host, it was asked for %d times\n%s", asked, l.said.String())
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

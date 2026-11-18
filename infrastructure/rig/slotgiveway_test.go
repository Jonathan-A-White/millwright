package rig_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aGiveWay is a host that gave way once, from from to to: what its give-way record
// says (application.GiveWaySpans).
type aGiveWay struct{ from, to time.Time }

func (g aGiveWay) GaveWayBetween(_ context.Context, from, to time.Time) time.Duration {
	start, end := g.from, g.to
	if from.After(start) {
		start = from
	}
	if to.Before(end) {
		end = to
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

// freezeAt makes the clock jump by frozen at its first tick at or past at: the
// host gave way and stopped the waiter, and the clocks went on without it. The
// host's give-way record says so. then runs at every tick after.
func freezeAt(clock *aClock, at time.Time, frozen time.Duration, then func(time.Time)) aGiveWay {
	froze := false
	clock.onTick = func(now time.Time) {
		if !froze && !now.Before(at) {
			froze = true
			clock.at = now.Add(frozen)
		}
		if then != nil {
			then(clock.at)
		}
	}
	return aGiveWay{from: at, to: at.Add(frozen)}
}

// A host that gives way stops the waiter for the span: it waited for nobody then,
// so a give-way longer than the per-holder wait does not end the wait on waking
// (mw-gq6.1477, found by mw-gq6.1458).
func TestASlotWaitDoesNotCountAGiveWay(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-landing")
	clock := &aClock{at: anEpoch}
	release := anEpoch.Add(45 * time.Minute)
	gave := freezeAt(clock, anEpoch.Add(5*time.Minute), 30*time.Minute, func(at time.Time) {
		if !at.Before(release) {
			held.release()
		}
	})
	slots := rig.NewSlots(rig.WithSlotWait(20*time.Minute), rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep), rig.WithSlotGaveWay(gave))

	taken, err := slots.Take(context.Background(), dir, "checking mw-me")
	if err != nil {
		t.Fatalf("expected a slot wait to go on past a give-way longer than its per-holder wait, got %v", err)
	}
	defer taken.Release(context.Background())
	if waited := clock.at.Sub(anEpoch); waited != 45*time.Minute {
		t.Errorf("expected the slot to be taken when its holder let go, after 45m, waited %s", waited)
	}
}

// What the host gave way is not counted, but the rest of the wait is: a stuck
// holder is still given up on once the wait has passed outside the give-way.
func TestASlotWaitDoesNotCountAGiveWayButCountsTheRest(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHand(t, dir, "closing out mw-stuck")
	defer held.release()
	clock := &aClock{at: anEpoch}
	gave := freezeAt(clock, anEpoch.Add(5*time.Minute), 30*time.Minute, nil)
	slots := rig.NewSlots(rig.WithSlotWait(20*time.Minute), rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep), rig.WithSlotGaveWay(gave))

	_, err := slots.Take(context.Background(), dir, "checking mw-me")
	if err == nil {
		t.Fatal("expected a stuck holder to be given up on once the wait passed outside the give-way")
	}
	if waited := clock.at.Sub(anEpoch); waited != 50*time.Minute {
		t.Errorf("expected the waiter to give up after the 20m wait and the 30m give-way, waited %s", waited)
	}
}

// The overall cap does not count a give-way either: a live holder is waited on for
// the cap of time the waiter was awake.
func TestASlotWaitDoesNotCountAGiveWayAgainstTheCap(t *testing.T) {
	dir := aRigDir(t)
	held := holdSlotByHandAs(t, dir, "closing out mw-landing", os.Getpid())
	clock := &aClock{at: anEpoch}
	release := anEpoch.Add(2*time.Hour + 30*time.Minute)
	gave := freezeAt(clock, anEpoch.Add(time.Hour), time.Hour, func(at time.Time) {
		if !at.Before(release) {
			held.release()
		}
	})
	slots := rig.NewSlots(rig.WithSlotWait(20*time.Minute), rig.WithSlotCap(2*time.Hour), rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(clock.now, clock.sleep), rig.WithSlotGaveWay(gave))

	taken, err := slots.Take(context.Background(), dir, "checking mw-me")
	if err != nil {
		t.Fatalf("expected the cap not to count the hour the host gave way, got %v", err)
	}
	defer taken.Release(context.Background())
	if waited := clock.at.Sub(anEpoch); waited != 2*time.Hour+30*time.Minute {
		t.Errorf("expected the slot to be taken when its holder let go, after 2h30m, waited %s", waited)
	}
}

package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1316: on the Laptop `mw land` and `mw check` stalled each other for the
// whole of the check's 40-minute wait. A landing holds the rig's merge slot alone
// and gives it up to shared for a wait of its own (a wait for the gate lock, a
// wait for a calm host); a whole check takes the slot shared and then waits for
// the gate lock. The landing got the gate lock, asked to take the slot alone
// again, and so waited for the check, which waited for the gate lock the landing
// held. The one lock order is merge slot, then gate lock: a landing that has the
// gate lock and must have the slot alone gives the gate lock back first.

// aLandingAndACheckOfOneRig is a landing that holds the rig's merge slot alone
// and yields it for its waits, as mw land does (batchSlot), and a whole check of
// the same rig on the same host, over the real gate lock and merge slots on
// temporary directories, so that the order they take the two in is the real one.
type aLandingAndACheckOfOneRig struct {
	t *testing.T
	*aWholeCheckBesideTheHostsGateLock
	holding application.Holding
	yielded bool
	said    lockedBuffer
}

func newALandingAndACheckOfOneRig(t *testing.T) *aLandingAndACheckOfOneRig {
	t.Helper()
	l := &aLandingAndACheckOfOneRig{t: t, aWholeCheckBesideTheHostsGateLock: newWholeCheckBesideTheGateLock(t, &scriptedChecks{passes: []bool{true}})}
	holding, err := l.slots.Take(context.Background(), l.rigDir, "mw land on laptop, landing a batch of millwright")
	if err != nil {
		t.Fatal(err)
	}
	l.holding = holding
	return l
}

// yield is Next.SlotYield, as batchSlot.yield does it: the slot shared for the
// wait, and alone again when the wait is over.
func (l *aLandingAndACheckOfOneRig) yield(ctx context.Context) func() {
	y, ok := l.holding.(application.YieldingHolding)
	if !ok {
		l.t.Error("expected the real merge slot to be one a landing can yield")
		return func() {}
	}
	if l.yielded {
		return func() {}
	}
	if err := y.Yield(ctx); err != nil {
		l.t.Errorf("the slot could not be yielded: %v", err)
		return func() {}
	}
	l.yielded = true
	return func() {
		l.yielded = false
		if err := y.Resume(ctx); err != nil {
			l.t.Errorf("the slot could not be taken alone again: %v", err)
		}
	}
}

func (l *aLandingAndACheckOfOneRig) land() application.BatchLand {
	return application.BatchLand{Next: application.Next{
		Tracker: apptest.NewFakeTracker(), Vault: newFakeVault(), Checks: &scriptedChecks{passes: []bool{true}},
		GateLock: l.gate, SlotYield: l.yield, Host: "laptop", Seat: "builder",
		Out: writerOf(&l.said), Err: writerOf(&l.said),
	}}
}

func (l *aLandingAndACheckOfOneRig) report() *application.BatchReport {
	return &application.BatchReport{Rig: "millwright", Number: 528, Target: "main", Base: "origin/main",
		Merged: []application.BatchStory{{ID: "mw-x.1"}}}
}

// startCheck runs a whole check of the rig in the background; its wait is a few
// seconds, which a deadlock outlasts and a working order never reads.
func (l *aLandingAndACheckOfOneRig) startCheck() <-chan checkEnded {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	ended := make(chan checkEnded, 1)
	go func() {
		report, err := application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: l.checks,
			Slot: l.slots, GateLock: l.gate, Records: l.records, PartSlotWait: 3 * time.Second,
			GateNoticeEvery: time.Hour, Host: "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": l.rigDir},
			Out:  writerOf(l.told),
		}.Run(context.Background(), "mw-x.1")
		ended <- checkEnded{report: report, err: err}
	}()
	return ended
}

type checkEnded struct {
	report application.CheckReport
	err    error
}

// waitQueued waits for the gate lock's queue to hold n tickets.
func (l *aLandingAndACheckOfOneRig) waitQueued(n int) {
	l.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); l.queuedGate() < n; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			l.t.Fatalf("expected %d in the gate lock's queue, got %d", n, l.queuedGate())
		}
	}
}

// finishes fails the test unless the landing's gate and the check both end, with
// the tests passed, inside a second.
func (l *aLandingAndACheckOfOneRig) finishes(gated <-chan application.Checked, checked <-chan checkEnded, began time.Time) {
	l.t.Helper()
	select {
	case gate := <-gated:
		if !gate.Passed {
			l.t.Errorf("expected the landing's gate to pass, got %+v\n%s", gate, l.said.String())
		}
	case <-time.After(5 * time.Second):
		l.t.Fatalf("expected the landing's gate to end: it and the check wait on each other\nlanding said:\n%s\ncheck said:\n%s", l.said.String(), l.told.String())
	}
	select {
	case ended := <-checked:
		if ended.err != nil {
			l.t.Errorf("expected the check to pass, got %v\n%s\ncheck said:\n%s", ended.err, ended.report, l.told.String())
		}
	case <-time.After(5 * time.Second):
		l.t.Fatalf("expected the check to end\ncheck said:\n%s", l.told.String())
	}
	if took := time.Since(began); took > time.Second {
		l.t.Errorf("expected both to end inside a second, they took %s\nlanding said:\n%s\ncheck said:\n%s", took, l.said.String(), l.told.String())
	}
	if holder := l.gate.Holder(); holder != "" {
		l.t.Errorf("expected the gate lock to be free after both, %q holds it", holder)
	}
}

// The landing waits for the gate lock another gate holds, with its slot shared; a
// whole check of the rig takes the slot shared and queues for the lock behind it;
// the lock is freed and the landing is first. It must not wait, holding the lock,
// for the check, which waits for the lock.
func TestALandingThatWaitedForTheGateLockSharedAndAWholeCheckQueuedBehindItBothEnd(t *testing.T) {
	l := newALandingAndACheckOfOneRig(t)
	defer l.holding.Release(context.Background())
	other, err := l.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}

	began := time.Now()
	gated := make(chan application.Checked, 1)
	go func() { gated <- l.land().GateTheBatch(context.Background(), l.rigDir, l.report()) }()
	l.waitQueued(1)
	checked := l.startCheck()
	l.waitQueued(2)
	if err := other.Holding.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.finishes(gated, checked, began)
}

// The landing holds the gate lock and waits for the host to calm with its slot
// shared; a whole check takes the slot shared and queues for the lock. The wait
// ends: the landing must not wait for the check, holding the lock.
func TestALandingThatWaitedForACalmHostHoldingTheGateLockAndAWholeCheckQueuedBothEnd(t *testing.T) {
	l := newALandingAndACheckOfOneRig(t)
	defer l.holding.Release(context.Background())

	var checked <-chan checkEnded
	land := l.land()
	land.Next.Load = &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}}
	land.Next.LoadWait = func(context.Context, time.Duration) error {
		checked = l.startCheck()
		// The check is queued for the lock behind the landing when the slot was
		// given up; if the slot was not, it waits for the slot and is never queued.
		for deadline := time.Now().Add(200 * time.Millisecond); l.queuedGate() < 1 && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		}
		return nil
	}

	began := time.Now()
	gated := make(chan application.Checked, 1)
	go func() { gated <- land.GateTheBatch(context.Background(), l.rigDir, l.report()) }()
	var gate application.Checked
	select {
	case gate = <-gated:
	case <-time.After(5 * time.Second):
		t.Fatalf("expected the landing's gate to end\nlanding said:\n%s\ncheck said:\n%s", l.said.String(), l.told.String())
	}
	if !gate.Passed {
		t.Errorf("expected the landing's gate to pass, got %+v", gate)
	}
	// The landing is done with the slot: a check that waited for it goes on.
	if err := l.holding.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan application.Checked, 1)
	done <- gate
	l.finishes(done, checked, began)
}

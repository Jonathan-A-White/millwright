package application_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// takeOrder is the order the host's gate lock went to its takers in.
type takeOrder struct {
	mu    sync.Mutex
	order []string
}

func (o *takeOrder) took(who string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.order = append(o.order, who)
}

func (o *takeOrder) said() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.order...)
}

// testsNotingTheTake are the rig's tests, which note in the take order that the
// check had the lock when they ran.
type testsNotingTheTake struct{ order *takeOrder }

func (c testsNotingTheTake) Run(context.Context, string, string) (application.Checked, error) {
	c.order.took("the check")
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

// A check that asked for the host's gate lock before two other holders takes it
// before either of them, though it wins the lock on a busy host and gives it back
// while it waits for the host to calm (mw-gq6.1534). Before, it took the lock again
// behind whoever had queued meanwhile: on the Laptop a millwright story's check
// that won the lock at load 53 of 20 gave it back, and the checks and close-outs
// that asked after it went first, until its 40m wait ran out having run nothing
// (mw-gq6.1521, six attempts).
func TestACheckThatAskedFirstTakesTheGateLockBeforeTwoLaterAskersThoughItGaveItBackForACalmHost(t *testing.T) {
	order := &takeOrder{}
	w := newWholeCheckBesideTheGateLock(t, testsNotingTheTake{order: order})
	holder, err := w.gate.Take(context.Background(), "mw-5r3p30.1206", nil)
	if err != nil {
		t.Fatal(err)
	}

	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	type ended struct {
		report application.CheckReport
		err    error
	}
	checked := make(chan ended, 1)
	go func() {
		report, err := application.Check{
			Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: w.checks,
			Slot: w.slots, GateLock: w.gate, Records: w.records, PartSlotWait: hangGuard,
			GateNoticeEvery: time.Hour, CallNow: w.clock.Now,
			// The host is busy when the check wins the lock, and calm after one step
			// of the wait, which the call's clock is moved by.
			Load:     &apptest.FakeHostLoad{Readings: []application.LoadReading{busy(), calm()}},
			LoadPoll: 30 * time.Second,
			LoadWait: func(_ context.Context, d time.Duration) error { w.clock.Pass(d); return nil },
			Host:     "laptop", Remote: "origin",
			Rigs: map[string]string{"millwright": w.rigDir},
			Out:  writerOf(w.told),
		}.Run(context.Background(), "mw-x.1")
		checked <- ended{report, err}
	}()
	waitForQueued(t, w, 1)

	// Two other holders ask after the check: a lampas story's close-out and another
	// story's check.
	var later sync.WaitGroup
	for i, who := range []string{"mw-cvwm1u.22", "@laptop checking mw-gq6.1445"} {
		later.Add(1)
		go func() {
			defer later.Done()
			turn, err := w.gate.Take(context.Background(), who, nil)
			if err != nil {
				t.Errorf("expected %s to take the gate lock in the end, got %v", who, err)
				return
			}
			order.took(who)
			time.Sleep(10 * time.Millisecond)
			turn.Holding.Release(context.Background())
		}()
		waitForQueued(t, w, i+2)
	}

	holder.Holding.Release(context.Background())
	var got ended
	select {
	case got = <-checked:
	case <-time.After(hangGuard):
		t.Fatalf("expected the check to end, it is still waiting:\n%s", w.told.String())
	}
	later.Wait()

	if got.err != nil || !got.report.Passed() {
		t.Fatalf("expected the check to pass once the host calmed, got %v and %+v\n%s", got.err, got.report.Refusals, w.told.String())
	}
	if took := order.said(); len(took) != 3 || took[0] != "the check" {
		t.Errorf("expected the check, which asked first, to take the gate lock before the two who asked after it; the lock went to %q\n%s", took, w.told.String())
	}
	if !strings.Contains(w.told.String(), "gives laptop's gate lock back while it waits for the host to calm before the rig's tests, and takes it again in the place it had in the queue, ahead of whoever asked after it") {
		t.Errorf("expected the check to give the gate lock back for the calm wait keeping its place, it said:\n%s", w.told.String())
	}
	if n := w.queuedGate(); n != 0 {
		t.Errorf("expected no ticket left in the gate queue once all are done, got %d", n)
	}
}

// waitForQueued waits until n tickets stand in the gate queue.
func waitForQueued(t *testing.T, w *aWholeCheckBesideTheHostsGateLock, n int) {
	t.Helper()
	for deadline := time.Now().Add(hangGuard); w.queuedGate() < n; {
		if time.Now().After(deadline) {
			t.Fatalf("expected %d ticket(s) in the gate queue, there are %d", n, w.queuedGate())
		}
		time.Sleep(time.Millisecond)
	}
}

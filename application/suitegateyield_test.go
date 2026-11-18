package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
	"github.com/Jonathan-A-White/millwright/infrastructure/ticketq"
)

// aCloseOutQueuedForTheGate puts a ticket of a close-out in the gate queue with the
// lock itself free, as a close-out that has been given a place and has not yet
// taken the lock is: the ticket stands until the returned func drops it.
func aCloseOutQueuedForTheGate(t *testing.T, w *aSuiteBesideTheHostsGateLock, holder string) (leave func()) {
	t.Helper()
	queue := &ticketq.Queue{Dir: filepath.Join(w.dir, hostlock.GateQueueDir), Cap: hostlock.GateCap, Now: time.Now}
	if err := os.MkdirAll(queue.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ticket, err := queue.Join(holder, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ticket.Drop
}

func untilSaid(t *testing.T, w *aSuiteBesideTheHostsGateLock, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(w.told.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("expected the suite to say %q, it said %q", want, w.told.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestASuiteDoesNotTakeTheGateLockWhileACloseOutIsQueuedForItAndTakesItOnceTheQueueIsEmpty(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	leave := aCloseOutQueuedForTheGate(t, w, "mw-gq6.1499")
	suite := w.suite()
	suite.GateYieldPoll = 5 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		_, err := suite.Run(context.Background(), "lampas")
		done <- err
	}()

	untilSaid(t, w, "waiting: close-outs queued for the gate")
	time.Sleep(100 * time.Millisecond)
	if ran := w.runner.Ran(); len(ran) != 0 {
		t.Fatalf("expected the suite to run nothing while a close-out was queued for the gate, it ran %+v", ran)
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the suite not to take the gate lock while a close-out was queued, %q holds it", holder)
	}

	leave()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected the suite to pass once the queue was empty, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("expected the suite to take the gate lock once no close-out was queued")
	}
	if ran := w.runner.Ran(); len(ran) != 1 {
		t.Errorf("expected the suite to run once the queue was empty, it ran %+v", ran)
	}
}

func TestASuiteTakesTheGateLockAsBeforeWhenNoCloseOutIsQueued(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	suite := w.suite()
	suite.GateYieldPoll = 5 * time.Millisecond
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.told.String(), "close-outs queued") {
		t.Errorf("expected the suite not to wait for a queue holding no close-out, it said %q", w.told.String())
	}
	if ran := w.runner.Ran(); len(ran) != 1 {
		t.Errorf("expected the suite to run, it ran %+v", ran)
	}
}

func TestASuiteFinishesTheCommandLineItHoldsTheLockForThenGivesWayToACloseOutQueuedMeanwhile(t *testing.T) {
	testsRunning, finishTests := make(chan struct{}), make(chan struct{})
	specsRunning := make(chan struct{})
	w := newSuiteBesideTheGateLock(t, func(_, command string) (application.Checked, error) {
		if command == "npm test" {
			close(testsRunning)
			<-finishTests
		} else {
			close(specsRunning)
		}
		return application.Checked{Passed: true}, nil
	})
	suite := aSuiteOfTwoPhases(w)
	suite.GateYieldPoll = 5 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		_, err := suite.Run(context.Background(), "lampas")
		done <- err
	}()
	select {
	case <-testsRunning:
	case <-time.After(10 * time.Second):
		t.Fatal("expected the suite to start the rig's tests")
	}

	// A close-out queues while the suite's line runs: the line is not cut short.
	leave := aCloseOutQueuedForTheGate(t, w, "mw-gq6.1478")
	if holder := w.gate.Holder(); holder != "suite lampas" {
		t.Errorf("expected the suite to go on holding the lock for its line, %q holds it", holder)
	}
	close(finishTests)

	untilSaid(t, w, "waiting: close-outs queued for the gate")
	select {
	case <-specsRunning:
		t.Fatal("expected the suite's changed specs to wait for the close-out ahead of them")
	case <-time.After(150 * time.Millisecond):
	}
	leave()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected the suite to pass, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("expected the suite to run its changed specs once the queue was empty")
	}
	if ran := w.runner.Ran(); len(ran) != 2 {
		t.Errorf("expected the tests and the changed specs to run, it ran %+v", ran)
	}
}

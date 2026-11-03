package application_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// aSuiteBesideTheHostsGateLock is mw suite on a rig, with this host's gate lock
// a close-out's gate may hold (mw-gq6.914).
type aSuiteBesideTheHostsGateLock struct {
	gate   *hostlock.Gate
	dir    string
	runner *apptest.FakeSuiteRunner
	trees  *apptest.FakeSuiteTrees
	told   *lockedBuffer
}

func newSuiteBesideTheGateLock(t *testing.T, respond func(dir, command string) (application.Checked, error)) *aSuiteBesideTheHostsGateLock {
	t.Helper()
	dir := t.TempDir()
	return &aSuiteBesideTheHostsGateLock{
		gate:   hostlock.NewGate(dir, hostlock.WithGatePoll(5*time.Millisecond)),
		dir:    dir,
		runner: &apptest.FakeSuiteRunner{Respond: respond},
		trees:  &apptest.FakeSuiteTrees{},
		told:   &lockedBuffer{},
	}
}

// queued waits until a gate is waiting in the gate lock's queue behind the
// holder, so that what follows is decided by who is queued and not by a sleep
// that a loaded host can outrun (mw-gq6.921).
func (w *aSuiteBesideTheHostsGateLock) queued(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(filepath.Join(w.dir, hostlock.GateQueueDir))
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("expected a gate to be queued for the host's gate lock")
}

func (w *aSuiteBesideTheHostsGateLock) run(ctx context.Context) (application.SuiteReport, error) {
	return w.suite().Run(ctx, "lampas")
}

func (w *aSuiteBesideTheHostsGateLock) suite() application.Suite {
	heads := apptest.NewFakeRigHeads()
	heads.SetHead("/rigs/lampas", "11541bc0d")
	return application.Suite{
		Trees: w.trees, Heads: heads, Runner: w.runner, Records: &apptest.FakeSuiteRecords{},
		GateLock: w.gate, GateNoticeEvery: 40 * time.Millisecond,
		Now:   func() time.Time { return time.Date(2026, 11, 3, 8, 0, 1, 0, time.UTC) },
		Host:  "laptop",
		Rigs:  map[string]string{"lampas": "/rigs/lampas"},
		Tests: map[string]string{"lampas": "npm test"},
		Out:   writerOf(w.told),
	}
}

func TestACloseOutsGateHoldingTheHostsGateLockMakesMwSuiteWaitAndRunOnceItIsGivenBack(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	gate, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := w.run(context.Background())
		done <- err
	}()

	// While the close-out's gate holds the lock the suite waits, says so, names the holder
	// and runs nothing: it does not run beside the gate.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(w.told.String(), "still waiting") {
		if time.Now().After(deadline) {
			t.Fatalf("expected the suite to say it is still waiting for the gate lock, it said %q", w.told.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if said := w.told.String(); !strings.Contains(said, "mw-gq6.894") {
		t.Errorf("expected the suite to name the gate that holds the lock, it said %q", said)
	}
	if ran := w.runner.Ran(); len(ran) != 0 {
		t.Fatalf("expected the suite to run nothing while a gate held the lock, it ran %+v", ran)
	}
	if got := len(w.trees.Added()); got != 0 {
		t.Errorf("expected the suite to cut no worktree while it waited, it cut %d", got)
	}

	gate.Holding.Release(context.Background())
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected the suite to pass once the lock was free, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the suite to run once the close-out's gate gave the lock back")
	}
	if ran := w.runner.Ran(); len(ran) != 1 || ran[0].Command != "npm test" {
		t.Errorf("expected the suite to run npm test once, it ran %+v", ran)
	}
}

func TestAMwSuiteHoldingTheHostsGateLockMakesACloseOutsGateWaitAndGivesItBack(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	w := newSuiteBesideTheGateLock(t, func(_, _ string) (application.Checked, error) {
		close(started)
		<-release
		return application.Checked{Passed: true}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := w.run(context.Background())
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the suite to start the rig's tests")
	}

	var heldBy string
	waitCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := w.gate.Take(waitCtx, "mw-gq6.894", func(by string) { heldBy = by }); err == nil {
		t.Fatal("expected a close-out's gate to wait while the suite held the host's gate lock")
	}
	if !strings.Contains(heldBy, "suite") || !strings.Contains(heldBy, "lampas") {
		t.Errorf("expected the close-out to be told the suite of lampas holds the lock, got %q", heldBy)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("expected the suite to pass, got %v", err)
	}
	turn, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatalf("expected the gate lock to be free once the suite ended: %v", err)
	}
	turn.Holding.Release(context.Background())
}

func TestAMwSuiteThatCannotTakeTheHostsGateLockRunsNothingAndSaysWhoHoldsIt(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	gate, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Holding.Release(context.Background())

	// A wait that has been given up by the time it is made: the lock is held, so
	// it cannot be taken, however long the host takes to get there.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = w.run(ctx)
	if err == nil {
		t.Fatal("expected a suite that could not take the gate lock to end in an error, not to run beside the gate")
	}
	if !strings.Contains(err.Error(), "gate lock") || !strings.Contains(err.Error(), "mw-gq6.894") {
		t.Errorf("expected the error to say the gate lock is held and by whom, got %q", err)
	}
	if ran := w.runner.Ran(); len(ran) != 0 {
		t.Errorf("expected the suite to run nothing, it ran %+v", ran)
	}
}

func TestAMwSuiteThatFailsGivesTheHostsGateLockBack(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, func(_, _ string) (application.Checked, error) {
		return application.Checked{Passed: false, Output: "FAIL\n"}, nil
	})
	if _, err := w.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the gate lock to be given back when the suite ended, %q still holds it", holder)
	}
}

func TestAMwSuiteWaitingForACalmHostDoesNotHoldTheHostsGateLock(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	suite := w.suite()
	suite.Load = &apptest.FakeHostLoad{Readings: []application.LoadReading{
		{Load: 40, Cores: 20}, {Load: 30, Cores: 20}, {Load: 3, Cores: 20},
	}}
	suite.LoadPoll = time.Second
	var heldWhileWaiting []string
	waits := 0
	suite.LoadWait = func(context.Context, time.Duration) error {
		waits++
		heldWhileWaiting = append(heldWhileWaiting, w.gate.Holder())
		return nil
	}
	if _, err := suite.Run(context.Background(), "lampas"); err != nil {
		t.Fatal(err)
	}
	if waits != 2 {
		t.Fatalf("expected the suite to wait twice for the host to calm, it waited %d times", waits)
	}
	for i, holder := range heldWhileWaiting {
		if holder != "" {
			t.Errorf("expected the suite to wait for a calm host without the gate lock, wait %d found %q holding it", i+1, holder)
		}
	}
	if ran := w.runner.Ran(); len(ran) != 1 {
		t.Errorf("expected the suite to run once the host was calm, it ran %+v", ran)
	}
}

// aSuiteOfTwoPhases is a suite that runs the rig's tests and then its changed
// specs, the second held back by held until it is released.
func aSuiteOfTwoPhases(w *aSuiteBesideTheHostsGateLock) application.Suite {
	suite := w.suite()
	suite.ChangedSpecs = map[string]string{"lampas": "npx playwright test {specs}"}
	return suite
}

func TestAMwSuiteGivesTheHostsGateLockBackBetweenItsPhasesSoACloseOutWaitsForOneStepAndNotTheWholeSuite(t *testing.T) {
	testsRunning, finishTests := make(chan struct{}), make(chan struct{})
	specsRunning := make(chan struct{})
	var holderAtSpecs string
	w := newSuiteBesideTheGateLock(t, func(_, command string) (application.Checked, error) {
		if command == "npm test" {
			close(testsRunning)
			<-finishTests
		} else {
			holderAtSpecs = "run"
			close(specsRunning)
		}
		return application.Checked{Passed: true}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := aSuiteOfTwoPhases(w).Run(context.Background(), "lampas")
		done <- err
	}()
	select {
	case <-testsRunning:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the suite to start the rig's tests")
	}

	// A close-out's gate comes while the tests run and waits for them.
	took := make(chan application.GateTurn, 1)
	go func() {
		turn, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
		if err != nil {
			t.Error(err)
		}
		took <- turn
	}()
	w.queued(t)
	close(finishTests)

	// It is next: the suite's changed specs wait behind it, though the suite had
	// the lock a moment before.
	var turn application.GateTurn
	select {
	case turn = <-took:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the close-out's gate to take the lock once the suite's tests had ended, not to wait for the changed specs")
	}
	select {
	case <-specsRunning:
		t.Fatal("expected the suite's changed specs not to run beside the close-out's gate")
	case <-time.After(150 * time.Millisecond):
	}
	if holder := w.gate.Holder(); holder != "mw-gq6.894" {
		t.Errorf("expected the close-out's gate to hold the lock, %q does", holder)
	}

	turn.Holding.Release(context.Background())
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected the suite to pass once the close-out's gate gave the lock back, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the suite to run its changed specs once the close-out's gate gave the lock back")
	}
	if holderAtSpecs == "" {
		t.Error("expected the suite to run its changed specs")
	}
	if holder := w.gate.Holder(); holder != "" {
		t.Errorf("expected the gate lock to be free once the suite ended, %q holds it", holder)
	}
}

func TestAMwSuiteHoldsTheHostsGateLockForEachOfItsCommandLines(t *testing.T) {
	var held []string
	var w *aSuiteBesideTheHostsGateLock
	w = newSuiteBesideTheGateLock(t, func(_, command string) (application.Checked, error) {
		held = append(held, command+" under "+w.gate.Holder())
		if command == "npm test" {
			return application.Checked{Passed: false, Output: "FAIL tests/a.test.ts > a\n"}, nil
		}
		return application.Checked{Passed: true}, nil
	})
	if _, err := aSuiteOfTwoPhases(w).Run(context.Background(), "lampas"); err != nil {
		t.Fatal(err)
	}
	if len(held) < 3 {
		t.Fatalf("expected the tests, the changed specs and the re-run of the failing file, got %v", held)
	}
	for _, line := range held {
		if !strings.HasSuffix(line, " under suite lampas") {
			t.Errorf("expected every command line to be run holding the gate lock, got %q", line)
		}
	}
}

func TestAMwSuiteThatCannotTakeTheLockBackBetweenPhasesEndsSayingWhatRanAndWhoHoldsIt(t *testing.T) {
	var w *aSuiteBesideTheHostsGateLock
	blocked := make(chan application.GateTurn, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w = newSuiteBesideTheGateLock(t, func(_, command string) (application.Checked, error) {
		// A close-out's gate queues and, the suite having given the lock back, holds
		// it; the suite's wait for it again is given up once it does.
		go func() {
			turn, _ := w.gate.Take(context.Background(), "mw-gq6.894", nil)
			blocked <- turn
			cancel()
		}()
		w.queued(t)
		return application.Checked{Passed: true}, nil
	})
	_, err := aSuiteOfTwoPhases(w).Run(ctx, "lampas")
	if blocker := <-blocked; blocker.Holding != nil {
		defer blocker.Holding.Release(context.Background())
	}
	if err == nil {
		t.Fatal("expected a suite that could not take the gate lock back to end in an error")
	}
	for _, want := range []string{"gate lock", "mw-gq6.894", "npm test", "changed specs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to say %q, got %q", want, err)
		}
	}
	if ran := w.runner.Ran(); len(ran) != 1 {
		t.Errorf("expected the suite to run its tests and no more, it ran %+v", ran)
	}
}

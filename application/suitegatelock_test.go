package application_test

import (
	"context"
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
	runner *apptest.FakeSuiteRunner
	trees  *apptest.FakeSuiteTrees
	told   *lockedBuffer
}

func newSuiteBesideTheGateLock(t *testing.T, respond func(dir, command string) (application.Checked, error)) *aSuiteBesideTheHostsGateLock {
	t.Helper()
	return &aSuiteBesideTheHostsGateLock{
		gate:   hostlock.NewGate(t.TempDir(), hostlock.WithGatePoll(5*time.Millisecond)),
		runner: &apptest.FakeSuiteRunner{Respond: respond},
		trees:  &apptest.FakeSuiteTrees{},
		told:   &lockedBuffer{},
	}
}

func (w *aSuiteBesideTheHostsGateLock) run(ctx context.Context) (application.SuiteReport, error) {
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
	}.Run(ctx, "lampas")
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

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
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

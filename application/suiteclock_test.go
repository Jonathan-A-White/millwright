package application_test

import (
	"context"
	"encoding/json"
	"github.com/Jonathan-A-White/millwright/application"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// aClockThatStepsBy is an injected clock that moves on by step at each reading,
// so that a wait read off it is the number of readings and not the host's time
// (mw-gq6.1218).
func aClockThatStepsBy(step time.Duration) func() time.Time {
	var readings atomic.Int64
	start := time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	return func() time.Time {
		return start.Add(time.Duration(readings.Add(1)-1) * step)
	}
}

func TestAMwSuiteReadsWhatItHasWaitedForTheGateLockOffItsInjectedClock(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	gate, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Holding.Release(context.Background())
	suite := w.suite()
	suite.Now = aClockThatStepsBy(2 * time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		suite.Run(ctx, "lampas")
	}()
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(w.told.String(), "so far)") {
		if time.Now().After(deadline) {
			t.Fatalf("expected the suite to say it is still waiting for the gate lock, it said %q", w.told.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	said := w.told.String()
	if strings.Contains(said, "waited under a second so far") || !strings.Contains(said, "(waited 2m so far)") {
		t.Errorf("expected the wait said to be the injected clock's two minutes, it said %q", said)
	}
}

func TestAMwSuiteThatCannotTakeTheGateLockSaysHowLongOnItsInjectedClockItWaited(t *testing.T) {
	w := newSuiteBesideTheGateLock(t, nil)
	gate, err := w.gate.Take(context.Background(), "mw-gq6.894", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Holding.Release(context.Background())
	suite := w.suite()
	suite.GateNoticeEvery = time.Hour
	suite.Now = aClockThatStepsBy(2 * time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = suite.Run(ctx, "lampas")
	if err == nil {
		t.Fatal("expected a suite that could not take the gate lock to end in an error")
	}
	if !strings.Contains(err.Error(), "could not be taken after 2m") {
		t.Errorf("expected the error to say the injected clock's two minutes were waited, got %q", err)
	}
}

func TestTheRunLogOfASleepPassReadsItsElapsedTimeOffTheInjectedClock(t *testing.T) {
	f := newSleepFixture()
	step := aClockThatStepsBy(90 * time.Second)
	f.pass.Now = step
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	var log application.SleepRunLog
	for _, content := range f.vault.runs {
		if err := json.Unmarshal([]byte(content), &log); err != nil {
			t.Fatal(err)
		}
	}
	if log.Elapsed != "1m30s" {
		t.Errorf("expected the elapsed time to be the injected clock's 1m30s, the log says %q", log.Elapsed)
	}
}

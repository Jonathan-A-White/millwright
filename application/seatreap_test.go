package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// memoryLog is a reaper log held in memory.
type memoryLog struct{ lines []string }

func (m *memoryLog) Note(_ context.Context, _, line string) error {
	m.lines = append(m.lines, line)
	return nil
}

// stubborn is a terminal whose windows will not close.
type stubborn struct{ *apptest.FakeWindows }

func (stubborn) Close(context.Context, string) error { return errors.New("cannot kill the window") }

func aFinishedWindow(t *testing.T) (*apptest.FakeWindows, *fakeSeatFiles) {
	t.Helper()
	windows := apptest.NewFakeWindows()
	windows.HoldsWithID("@3", "mayor-2026-09-19-12", time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC))
	// The newest handoff was written an hour before noon, after the window opened.
	return windows, &fakeSeatFiles{start: aSeatStart(nil)}
}

func TestAWindowThatWillNotCloseIsLoggedAndFails(t *testing.T) {
	windows, files := aFinishedWindow(t)
	log := &memoryLog{}

	report, err := application.SeatReap{
		Seats: files, Terminal: stubborn{windows}, Log: log,
		Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
		Interval: time.Second, Limit: time.Minute,
		Sleep: func(context.Context, time.Duration) error { return nil },
	}.Run(context.Background())

	if err == nil || report.Outcome != application.ReapFailed {
		t.Fatalf("expected the watch to fail, it ended %q with %v", report.Outcome, err)
	}
	if len(log.lines) != 2 || !strings.Contains(log.lines[1], "closing the window failed: cannot kill the window") {
		t.Errorf("expected an armed line and a failure line, got %q", log.lines)
	}
}

func TestAWatchStopsWhenItsContextEndsAndLogsNoOutcome(t *testing.T) {
	windows, files := aFinishedWindow(t)
	log := &memoryLog{}
	ctx, cancel := context.WithCancel(context.Background())

	_, err := application.SeatReap{
		Seats: files, Terminal: windows, Log: log,
		Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
		Interval: time.Hour,
		Sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		},
	}.Run(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the watch to end with the context's error, got %v", err)
	}
	if len(log.lines) != 1 || len(windows.ClosedIDs()) != 0 {
		t.Errorf("expected only the armed line and nothing closed, got %q, closed %v", log.lines, windows.ClosedIDs())
	}
}

func TestAWatchNeedsItsSeatAndAValidName(t *testing.T) {
	windows, files := aFinishedWindow(t)
	for name, reap := range map[string]application.SeatReap{
		"no seat":    {Seat: "", Window: "@3"},
		"a bad seat": {Seat: "../mayor", Window: "@3"},
		"no ports":   {Seat: "mayor", Window: "@3"},
	} {
		reap.Seats, reap.Terminal, reap.Log = files, windows, &memoryLog{}
		if name == "no ports" {
			reap.Log = nil
		}
		if _, err := reap.Run(context.Background()); err == nil {
			t.Errorf("%s: expected the watch to be refused", name)
		}
	}
}

// aClock is a clock the reaper's sleeps move on, so that a watch of hours runs
// in no time.
type aClock struct{ now time.Time }

func (c *aClock) Now() time.Time { return c.now }
func (c *aClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return nil
}

// aWatchOfAWindowRunningNext is a reaper on a finished window in which a
// hand-run `mw next mw-86e9r1.25` is running, looking every 30 seconds for up
// to three hours.
func aWatchOfAWindowRunningNext(t *testing.T) (application.SeatReap, *apptest.FakeWindows, *aClock, *memoryLog) {
	t.Helper()
	windows, files := aFinishedWindow(t)
	windows.RunsNext("@3", "mw-86e9r1.25")
	clock := &aClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	log := &memoryLog{}
	return application.SeatReap{
		Seats: files, Terminal: windows, Log: log,
		Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
		Interval: 30 * time.Second, Limit: 3 * time.Hour,
		Now: clock.Now, Sleep: clock.Sleep,
	}, windows, clock, log
}

func TestAWindowRunningMwNextIsNotClosedUntilTheRunEnds(t *testing.T) {
	reap, windows, clock, log := aWatchOfAWindowRunningNext(t)
	endsAt := clock.now.Add(10 * time.Minute)
	sleep := reap.Sleep
	reap.Sleep = func(ctx context.Context, d time.Duration) error {
		if !clock.now.Before(endsAt) {
			windows.EndsNext("@3")
		}
		return sleep(ctx, d)
	}

	report, err := reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed once the run ended, it ended %q with %v", report.Outcome, err)
	}
	if closedAt := clock.now; closedAt.Before(endsAt) {
		t.Errorf("the window was closed at %s, before the run ended at %s", closedAt, endsAt)
	}
	waiting := 0
	for _, line := range log.lines {
		if strings.Contains(line, "waiting: mw next mw-86e9r1.25 runs in this window") {
			waiting++
		}
	}
	if waiting != 1 || len(log.lines) != 3 {
		t.Errorf("expected armed, one waiting line and closed, got %q", log.lines)
	}
}

func TestAWaitForMwNextGivesUpAfterNinetyMinutes(t *testing.T) {
	reap, windows, clock, log := aWatchOfAWindowRunningNext(t)
	armed := clock.now

	report, err := reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed past the cap, it ended %q with %v", report.Outcome, err)
	}
	if got := clock.now.Sub(armed); got < 90*time.Minute || got > 91*time.Minute {
		t.Errorf("expected the window closed about 90 minutes in, it was closed %s in", got)
	}
	if closed := windows.ClosedIDs(); len(closed) != 1 {
		t.Errorf("expected the window closed once, got %v", closed)
	}
	if len(log.lines) != 4 || !strings.Contains(log.lines[2], "gave up waiting for mw next mw-86e9r1.25 after 1h30m0s") || !strings.Contains(log.lines[3], "closed") {
		t.Errorf("expected armed, waiting, gave up waiting and closed, got %q", log.lines)
	}
}

func TestAWindowWithNoMwNextIsReapedAsAlways(t *testing.T) {
	reap, windows, _, log := aWatchOfAWindowRunningNext(t)
	windows.EndsNext("@3")

	report, err := reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed, it ended %q with %v", report.Outcome, err)
	}
	if len(log.lines) != 2 || strings.Contains(strings.Join(log.lines, "\n"), "runs in this window") {
		t.Errorf("expected only an armed line and a closed line, got %q", log.lines)
	}
}

// theGateQueue is the gate's waiter record as a reaper reads it: one holder
// line for each live ticket, as the queue keeps them.
type theGateQueue struct{ holders []string }

func (q *theGateQueue) Holders() []string { return append([]string(nil), q.holders...) }

func TestAWindowWhoseMwNextWaitsForTheGateIsNotClosedPastTheWait(t *testing.T) {
	reap, windows, clock, log := aWatchOfAWindowRunningNext(t)
	armed := clock.now
	queue := &theGateQueue{holders: []string{"checking mw-86e9r1.25"}}
	reap.GateWaiters = queue.Holders
	// The run waits for the gate for two hours, past the 90 minutes, then ends.
	endsAt := armed.Add(2 * time.Hour)
	sleep := reap.Sleep
	reap.Sleep = func(ctx context.Context, d time.Duration) error {
		if !clock.now.Before(endsAt) {
			queue.holders = nil
			windows.EndsNext("@3")
		}
		return sleep(ctx, d)
	}

	report, err := reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed once the run ended, it ended %q with %v", report.Outcome, err)
	}
	if clock.now.Before(endsAt) {
		t.Errorf("the window was closed at %s, before the run ended at %s", clock.now, endsAt)
	}
	all := strings.Join(log.lines, "\n")
	if strings.Count(all, "waiting: mw next mw-86e9r1.25 waits for the gate") != 1 {
		t.Errorf("expected one line saying the run waits for the gate, got %q", log.lines)
	}
	if strings.Contains(all, "gave up waiting") {
		t.Errorf("a run waiting for the gate was given up on: %q", log.lines)
	}
	if closed := windows.ClosedIDs(); len(closed) != 1 {
		t.Errorf("expected the window closed once, got %v", closed)
	}
}

func TestAMwNextThatLeavesTheGateQueueIsGivenUpOnAfterTheWaitAsAlways(t *testing.T) {
	reap, _, clock, log := aWatchOfAWindowRunningNext(t)
	armed := clock.now
	// Another story's ticket is in the queue; this run's is not.
	reap.GateWaiters = func() []string { return []string{"checking mw-other.1"} }

	report, err := reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window closed past the cap, it ended %q with %v", report.Outcome, err)
	}
	if got := clock.now.Sub(armed); got < 90*time.Minute || got > 91*time.Minute {
		t.Errorf("expected the window closed about 90 minutes in, it was closed %s in", got)
	}
	if !strings.Contains(strings.Join(log.lines, "\n"), "gave up waiting for mw next mw-86e9r1.25") {
		t.Errorf("expected the wait given up on, got %q", log.lines)
	}
}

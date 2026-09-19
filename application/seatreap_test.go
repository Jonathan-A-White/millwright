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

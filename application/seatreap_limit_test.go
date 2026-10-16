package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aLimitScreen is a Claude Code screen parked on the usage-limit menu.
const aLimitScreen = "● Reading the newest handoff first.\n\n" +
	"  ⎿  You've hit your weekly limit · resets Oct 16, 7am (UTC)\n\n" +
	" │ What do you want to do?\n" +
	" │ ❯ 1. Stop and wait for limit to reset\n" +
	" │   2. Switch to usage credits\n"

// A session that has handed off and then stopped at the usage limit holds no
// draft and will never be idle again before the reset: the reaper closes it,
// saying so, rather than watching it until it gives up (mw-gq6.473).
func TestAFinishedWindowStoppedAtTheUsageLimitIsClosedSayingSo(t *testing.T) {
	windows, files := aFinishedWindow(t)
	if err := windows.Shows("@3", aLimitScreen); err != nil {
		t.Fatal(err)
	}
	log := &memoryLog{}
	clock := &aClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}

	report, err := application.SeatReap{
		Seats: files, Terminal: windows, Log: log,
		Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
		Interval: time.Second, Limit: time.Minute,
		Now: clock.Now, Sleep: clock.Sleep,
	}.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the window stopped at the limit to be closed, it ended %q with %v (log %q)", report.Outcome, err, log.lines)
	}
	if !strings.Contains(report.Said, "stopped at the usage limit") || !strings.Contains(report.Said, "You've hit your weekly limit") {
		t.Errorf("expected the closing line to say the pane was stopped at the usage limit, in the harness's words, got %q", report.Said)
	}
}

// The Deputy's reaper types its mail nudge into an idle pane; into a pane
// stopped at the usage limit it types nothing, since nothing typed there is
// worked before the reset, and the session has handed off.
func TestADeputyStoppedAtTheUsageLimitIsTypedNoNudge(t *testing.T) {
	r := newDeputyReaper(t, aBoxWith(t, 2))
	if err := r.windows.Shows("@3", aLimitScreen); err != nil {
		t.Fatal(err)
	}

	report, err := r.reap.Run(context.Background())

	if err != nil || report.Outcome != application.ReapClosed {
		t.Fatalf("expected the Deputy's window to be closed, it ended %q with %v (log %q)", report.Outcome, err, r.log.lines)
	}
	if typed := r.windows.Typed("@3"); len(typed) != 0 {
		t.Errorf("expected nothing typed into a pane at the limit, got %q", typed)
	}
}

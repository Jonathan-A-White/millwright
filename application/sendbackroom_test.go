package application_test

import (
	"errors"
	"io"

	"context"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1512: the sends of mw next, to rebase a branch that does not merge and to
// fix the tests of a merged result, start a Builder session. A host with no room
// (the memory floor here) starts none, as a host that gives way starts none: the
// story is handed back for its close-out alone, nothing is recorded as sent back,
// and the story says why.
func TestASendBackWaitsForRoom(t *testing.T) {
	for name, landing := range map[string]struct {
		lands application.Landing
		state string
	}{
		"merge conflict":    {&aConflictingLanding{fakeRetryLanding{AheadCount: 1}}, application.RebaseState},
		"merged tests fail": {&mergingLanding{fakeRetryLanding{AheadCount: 1}}, application.MergedTestsState},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			id := "mw-x.8"
			next := aCloseOutOn("laptop", id, aSlot{}, aCrossSlotOn(apptest.NewFakeTracker(), "laptop", new(atomic.Int32)),
				landing.lands, &scriptedChecks{passes: []bool{true, false}})
			load := &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.5, Cores: 4, MemKnown: true, MemAvailableMB: 120}}
			next.Load = load
			next.Room = application.RoomLimits{MinFreeMB: 500}

			report, err := next.Run(ctx, id)
			if err == nil {
				t.Fatalf("expected the close-out handed back, got %+v", report)
			}
			runner := next.Runner.(*apptest.FakeRunner)
			if names := runner.Names(); len(names) != 0 || report.SentBack != "" {
				t.Fatalf("sessions %v, sent back %q: a host with no room started a Builder", names, report.SentBack)
			}
			if !report.HandedBack || report.Reason != application.ReasonHostBusy || report.Landed || report.Refused {
				t.Fatalf("expected the close-out handed back for the host's room, got %+v", report)
			}
			tracker := next.Tracker.(*apptest.FakeTracker)
			if got := tracker.State(id, landing.state); got != "" {
				t.Fatalf("%s = %q: the send-back was recorded though nothing was sent", landing.state, got)
			}
			if got := tracker.State(id, application.RunState); got != application.RunCloseOutAgain {
				t.Fatalf("run state = %q, want %q", got, application.RunCloseOutAgain)
			}
			var said bool
			for _, c := range tracker.Comments(id) {
				said = said || strings.Contains(c, "120 MB free, under 500 MB")
			}
			if !said {
				t.Fatalf("no comment on the story says the host has no room: %q", tracker.Comments(id))
			}
		})
	}
}

// A host with room still sends the story back.
func TestASendBackOnAHostWithRoomStartsTheSession(t *testing.T) {
	id := "mw-x.8"
	next := aCloseOutOn("laptop", id, aSlot{}, aCrossSlotOn(apptest.NewFakeTracker(), "laptop", new(atomic.Int32)),
		&aConflictingLanding{fakeRetryLanding{AheadCount: 1}}, &scriptedChecks{passes: []bool{true, false}})
	next.Load = &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.5, Cores: 4, MemKnown: true, MemAvailableMB: 4000}}
	next.Room = application.RoomLimits{MinFreeMB: 500}
	report, _ := next.Run(context.Background(), id)
	if report.HandedBack || report.SentBack == "" {
		t.Fatalf("expected the story sent back on a host with room, got %+v", report)
	}
}

// The batch's fixer is a Builder session too: a host with no room starts none, and
// the batch is held with its stories queued, as for a host that gives way.
func TestABatchFixerWaitsForRoom(t *testing.T) {
	for name, tc := range map[string]struct {
		mem  int64
		want string
	}{"no room": {120, "was not started: the host has no room"}, "room": {4000, "could not be started: no tmux here"}} {
		t.Run(name, func(t *testing.T) {
			runner := apptest.NewFakeRunner()
			vault := newFakeVault()
			runner.StartErr = errors.New("no tmux here") // a fixer that is let start ends the test
			land := application.BatchLand{Next: application.Next{
				Landing: &fakeRetryLanding{}, Runner: runner,
				Boot: application.SeatBoot{Vault: vault, Harness: claude.New(), Seat: "builder", Host: "laptop"},
				Load: &apptest.FakeHostLoad{Reading: application.LoadReading{Load: 0.5, Cores: 4, MemKnown: true, MemAvailableMB: tc.mem}},
				Room: application.RoomLimits{MinFreeMB: 500}, Host: "laptop", Seat: "builder",
				Out: io.Discard, Err: io.Discard,
			}, Fix: true}
			report := &application.BatchReport{Rig: "millwright", Target: "main", Number: 4, Base: "base0123", Tested: true}
			land.FixTheBatch(context.Background(), "/rigs/millwright/.landing-1", report)
			if got := report.Fixer.Trouble; !strings.Contains(got, tc.want) {
				t.Fatalf("fixer trouble %q, want it to say %q (held %q)", got, tc.want, report.Held)
			}
			if tc.mem < 500 && !strings.Contains(report.Held, "120 MB free, under 500 MB") {
				t.Fatalf("the batch's hold does not say why: %q", report.Held)
			}
		})
	}
}

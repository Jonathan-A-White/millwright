package application_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// givingWayGate is a give-way gate that finds the host giving way every time it
// is asked, and counts the looks.
type givingWayGate struct {
	reads atomic.Int32
	since time.Time
}

func (g *givingWayGate) Standing(context.Context, bool) (application.GiveWayStanding, bool, error) {
	g.reads.Add(1)
	return application.GiveWayStanding{Since: g.since, By: "mw@laptop"}, true, nil
}

// brokenGate cannot read the marker.
type brokenGate struct{}

func (brokenGate) Standing(context.Context, bool) (application.GiveWayStanding, bool, error) {
	return application.GiveWayStanding{}, false, errors.New("the marker file is unreadable")
}

func TestGivesWayNowIsTheOneReadOfTheMarker(t *testing.T) {
	ctx := context.Background()
	since := time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC)
	if _, giving := application.GivesWayNow(ctx, nil, true); giving {
		t.Fatal("a nil gate gave way")
	}
	if _, giving := application.GivesWayNow(ctx, brokenGate{}, true); giving {
		t.Fatal("a marker that cannot be read gave way")
	}
	gate := &givingWayGate{since: since}
	standing, giving := application.GivesWayNow(ctx, gate, false)
	if !giving || gate.reads.Load() != 1 {
		t.Fatalf("giving %v after %d reads, want it to give way after one", giving, gate.reads.Load())
	}
	if got, want := standing.String(), "gives way since 2026-11-13T16:40:00Z (mw@laptop)"; got != want {
		t.Fatalf("standing says %q, want %q", got, want)
	}
}

// mw-2891blf.19 (.18): the sends of mw next, to rebase a branch that does not
// merge and to fix the tests of a merged result, start a Builder session. A host
// that began to give way after the Builder's session began starts none: the story
// is handed back for its close-out alone, as found, nothing is recorded as sent
// back, and the story says why.
func TestACloseOutOnAHostThatGivesWayStartsNoSendBackSession(t *testing.T) {
	for name, landing := range map[string]struct {
		lands  application.Landing
		state  string
		prefix string
	}{
		"merge conflict":    {&aConflictingLanding{fakeRetryLanding{AheadCount: 1}}, application.RebaseState, "rebase"},
		"merged tests fail": {&mergingLanding{fakeRetryLanding{AheadCount: 1}}, application.MergedTestsState, "merged tests"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			id := "mw-x.8"
			next := aCloseOutOn("laptop", id, aSlot{}, aCrossSlotOn(apptest.NewFakeTracker(), "laptop", new(atomic.Int32)),
				landing.lands, &scriptedChecks{passes: []bool{true, false}})
			gate := &givingWayGate{since: time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC)}
			next.GiveWay = gate

			report, err := next.Run(ctx, id)
			if err == nil {
				t.Fatalf("expected the close-out handed back, got %+v", report)
			}
			runner := next.Runner.(*apptest.FakeRunner)
			if names := runner.Names(); len(names) != 0 || report.SentBack != "" {
				t.Fatalf("sessions %v, sent back %q: a host that gives way started a Builder", names, report.SentBack)
			}
			if !report.HandedBack || report.Reason != application.ReasonGivesWay || report.Landed || report.Refused {
				t.Fatalf("expected the close-out handed back for the host giving way, got %+v", report)
			}
			if gate.reads.Load() == 0 {
				t.Fatal("the marker was never read")
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
				said = said || strings.Contains(c, "gives way since 2026-11-13T16:40:00Z (mw@laptop)")
			}
			if !said {
				t.Fatalf("no comment on the story says the host gives way: %q", tracker.Comments(id))
			}
		})
	}
}

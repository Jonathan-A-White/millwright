package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

type countingNudger struct {
	calls int
	err   error
}

func (c *countingNudger) Nudge(context.Context) error { c.calls++; return c.err }

func TestFollowCallsTheNudgerEveryPassAndSaysItsFailureOnce(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	head := &apptest.FakeHead{Heads: []string{"a"}}
	nudger := &countingNudger{err: errors.New("no terminal")}
	var errs bytes.Buffer
	err := application.EventFollow{
		Head:    head,
		Log:     &apptest.FakeEventLog{},
		Feed:    apptest.NewFakeTracker(),
		Cursors: &apptest.FakeFollowCursors{},
		Nudger:  nudger,
		Publish: func(context.Context) error { return nil },
		Sleep: func(context.Context, time.Duration) error {
			if nudger.calls >= 3 {
				stop()
				return ctx.Err()
			}
			return nil
		},
		Err: &errs,
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if nudger.calls != 3 || strings.Count(errs.String(), "telling the seats: no terminal") != 1 {
		t.Fatalf("the nudger was called %d times and the loop said %q", nudger.calls, errs.String())
	}
}

type countingSpringer struct {
	calls int
	err   error
}

func (c *countingSpringer) Spring(context.Context) error { c.calls++; return c.err }

func TestFollowCallsTheSpringerEveryPassAndSaysItsFailureOnce(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	springer := &countingSpringer{err: errors.New("no log")}
	var errs bytes.Buffer
	err := application.EventFollow{
		Head:     &apptest.FakeHead{Heads: []string{"a"}},
		Log:      &apptest.FakeEventLog{},
		Feed:     apptest.NewFakeTracker(),
		Cursors:  &apptest.FakeFollowCursors{},
		Springer: springer,
		Publish:  func(context.Context) error { return nil },
		Sleep: func(context.Context, time.Duration) error {
			if springer.calls >= 3 {
				stop()
				return ctx.Err()
			}
			return nil
		},
		Err: &errs,
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if springer.calls != 3 || strings.Count(errs.String(), "springing the jobs: no log") != 1 {
		t.Fatalf("the springer was called %d times and the loop said %q", springer.calls, errs.String())
	}
}

// slowShipper stands for a Postern send that hangs or fails after ~26 s
// (2026-10-07): it says how many nudges and springs had been made by the
// time Ship was entered, and then fails.
type slowShipper struct {
	nudger         *countingNudger
	springer       *countingSpringer
	nudgesAtShip   int
	springsAtShip  int
	ships          int
	stopAfterFirst context.CancelFunc
}

func (s *slowShipper) Ship(context.Context) error {
	s.ships++
	s.nudgesAtShip, s.springsAtShip = s.nudger.calls, s.springer.calls
	s.stopAfterFirst()
	return errors.New("said 502")
}

func TestFollowNudgesAndSpringsBeforeItShips(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	nudger, springer := &countingNudger{}, &countingSpringer{}
	shipper := &slowShipper{nudger: nudger, springer: springer, stopAfterFirst: stop}
	err := application.EventFollow{
		Head:     &apptest.FakeHead{Heads: []string{"a"}},
		Log:      &apptest.FakeEventLog{},
		Feed:     apptest.NewFakeTracker(),
		Cursors:  &apptest.FakeFollowCursors{},
		Shipper:  shipper,
		Nudger:   nudger,
		Springer: springer,
		Publish:  func(context.Context) error { return nil },
		Sleep:    func(context.Context, time.Duration) error { return ctx.Err() },
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shipper.ships != 1 {
		t.Fatalf("Ship was called %d times, want 1", shipper.ships)
	}
	if shipper.nudgesAtShip != 1 || shipper.springsAtShip != 1 {
		t.Fatalf("when Ship was entered the loop had nudged %d and sprung %d times; a slow send must not hold either back (want 1 and 1)",
			shipper.nudgesAtShip, shipper.springsAtShip)
	}
}

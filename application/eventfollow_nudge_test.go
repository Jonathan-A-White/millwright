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

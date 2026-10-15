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

type countingWatcher struct {
	calls int
	err   error
}

func (c *countingWatcher) Watch(context.Context) error { c.calls++; return c.err }

func TestFollowCallsTheSeatWatcherEveryPassAndSaysItsFailureOnce(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	watcher := &countingWatcher{err: errors.New("no windows")}
	var errs bytes.Buffer
	err := application.EventFollow{
		Head:    &apptest.FakeHead{Heads: []string{"a"}},
		Log:     &apptest.FakeEventLog{},
		Feed:    apptest.NewFakeTracker(),
		Cursors: &apptest.FakeFollowCursors{},
		Watcher: watcher,
		Publish: func(context.Context) error { return nil },
		Sleep: func(context.Context, time.Duration) error {
			if watcher.calls >= 3 {
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
	if watcher.calls != 3 || strings.Count(errs.String(), "watching the seats' context: no windows") != 1 {
		t.Fatalf("the watcher was called %d times and the loop said %q", watcher.calls, errs.String())
	}
}

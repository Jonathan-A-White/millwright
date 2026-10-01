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

// followRun runs the follow loop over head until it has read the head passes
// times, with a sleep that costs no time, and returns how many views it
// published and what it said on Err.
func followRun(t *testing.T, head *apptest.FakeHead, passes int, publish func(int) error) (published int, said string) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var errs bytes.Buffer
	follow := application.PosternViewFollow{
		Head: head,
		Publish: func(context.Context) error {
			published++
			return publish(published)
		},
		Every: time.Second,
		Sleep: func(context.Context, time.Duration) error {
			if head.Calls() >= passes {
				stop()
				return ctx.Err()
			}
			return nil
		},
		Err: &errs,
	}
	if err := follow.Run(ctx); err != nil {
		t.Fatalf("the loop ended with %v, want a clean stop", err)
	}
	return published, errs.String()
}

func nothingWrong(int) error { return nil }

func TestFollowPublishesOncePerChangeOfHeadAndNotOnAnUnchangedPass(t *testing.T) {
	head := &apptest.FakeHead{Heads: []string{"a", "a", "a", "b", "b", "c"}}
	published, said := followRun(t, head, 6, nothingWrong)
	if published != 3 {
		t.Fatalf("published %d views over heads a a a b b c, want 3 (a, b, c); said %q", published, said)
	}
}

func TestFollowPublishesTheFirstPassBecauseItHasSeenNoHead(t *testing.T) {
	head := &apptest.FakeHead{Heads: []string{"a"}}
	if published, _ := followRun(t, head, 3, nothingWrong); published != 1 {
		t.Fatalf("published %d views over an unchanging head, want 1", published)
	}
}

func TestAPublishErrorDoesNotStopTheLoopAndIsRetriedOnTheNextPass(t *testing.T) {
	head := &apptest.FakeHead{Heads: []string{"a", "a", "a", "b"}}
	published, said := followRun(t, head, 4, func(n int) error {
		if n == 1 {
			return errors.New("sealing failed")
		}
		return nil
	})
	// a fails, a is retried and goes through, a is then quiet, b publishes.
	if published != 3 {
		t.Fatalf("published %d times, want 3 (a failed, a retried, b); said %q", published, said)
	}
	if !strings.Contains(said, "sealing failed") {
		t.Fatalf("expected the error to be said on Err, got %q", said)
	}
}

func TestAHeadReadErrorIsSaidAndTheLoopGoesOn(t *testing.T) {
	head := &apptest.FakeHead{
		Heads: []string{"", "a", "a"},
		Errs:  []error{errors.New("bd could not be reached")},
	}
	published, said := followRun(t, head, 3, nothingWrong)
	if published != 1 {
		t.Fatalf("published %d views, want 1 once the head could be read", published)
	}
	if !strings.Contains(said, "bd could not be reached") {
		t.Fatalf("expected the read error to be said, got %q", said)
	}
}

func TestAnUnchangedFailureIsSaidOnceNotOnEveryPass(t *testing.T) {
	head := &apptest.FakeHead{Heads: []string{"a"}}
	_, said := followRun(t, head, 5, func(int) error { return errors.New("no key") })
	if n := strings.Count(said, "no key"); n != 1 {
		t.Fatalf("said the same failure %d times, want once:\n%s", n, said)
	}
}

func TestFollowStopsCleanlyWhenItsContextEnds(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	stop()
	follow := application.PosternViewFollow{
		Head:    &apptest.FakeHead{Heads: []string{"a"}},
		Publish: func(context.Context) error { t.Fatal("published after the stop"); return nil },
		Every:   time.Second,
	}
	if err := follow.Run(ctx); err != nil {
		t.Fatalf("expected a clean stop, got %v", err)
	}
}

func TestFollowWaitsEveryBetweenPasses(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	var slept []time.Duration
	follow := application.PosternViewFollow{
		Head:    &apptest.FakeHead{Heads: []string{"a"}},
		Publish: func(context.Context) error { return nil },
		Every:   1500 * time.Millisecond,
		Sleep: func(_ context.Context, d time.Duration) error {
			slept = append(slept, d)
			stop()
			return ctx.Err()
		},
	}
	if err := follow.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 1500*time.Millisecond {
		t.Fatalf("slept %v, want one 1.5s wait", slept)
	}
}

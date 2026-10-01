package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// followRun runs the follow loop over head until it has read the head passes
// times, with a sleep that costs no time, and returns how many views it
// published and what it said on Err.
func followRun(t *testing.T, head *apptest.FakeHead, passes int, publish func(int) error) (published int, said string) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var errs bytes.Buffer
	follow := application.EventFollow{
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
	follow := application.EventFollow{
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
	follow := application.EventFollow{
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

// eventsWorld is a follower over a fake tracker's beads, an event log in
// memory and a cursor in memory, with a head scripted pass by pass. between
// runs after each pass, before the follower sleeps; it is given the pass's
// number, counting from 1.
type eventsWorld struct {
	tracker *apptest.FakeTracker
	log     *apptest.FakeEventLog
	cursors *apptest.FakeFollowCursors
	t0      time.Time
	clock   time.Time
}

func newEventsWorld() *eventsWorld {
	t0 := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	tracker.SetBeadStates(t0,
		application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusOpen},
		application.BeadNow{ID: "mw-2", Type: "task", Status: application.StatusInProgress, Run: "running"},
	)
	return &eventsWorld{tracker: tracker, log: &apptest.FakeEventLog{}, cursors: &apptest.FakeFollowCursors{}, t0: t0}
}

func (w *eventsWorld) run(t *testing.T, heads []string, between func(pass int)) string {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	head := &apptest.FakeHead{Heads: heads}
	var errs bytes.Buffer
	follow := application.EventFollow{
		Head:    head,
		Feed:    w.tracker,
		Log:     w.log,
		Cursors: w.cursors,
		Publish: func(context.Context) error { return nil },
		Now:     w.now,
		Sleep: func(context.Context, time.Duration) error {
			if between != nil {
				between(head.Calls())
			}
			if head.Calls() >= len(heads) {
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
	return errs.String()
}

// now is the follower's clock: an hour after t0, unless a test moves it.
func (w *eventsWorld) now() time.Time {
	if w.clock.IsZero() {
		return w.t0.Add(time.Hour)
	}
	return w.clock
}

func (w *eventsWorld) at(seconds int) time.Time {
	return w.t0.Add(time.Duration(seconds) * time.Second)
}

func kindsOf(evs []events.Event) string {
	var out []string
	for _, e := range evs {
		out = append(out, fmt.Sprintf("%d:%s:%s:%s->%s:%s", e.Seq, e.Kind, e.Bead, e.From, e.To, e.Detail))
	}
	return strings.Join(out, " ")
}

func TestAPassTurnsAStatusChangeAndAnAnswerIntoTwoEventsAndAnUnchangedPassIntoNone(t *testing.T) {
	w := newEventsWorld()
	var afterChange, afterQuiet string
	said := w.run(t, []string{"a", "b", "b", "c"}, func(pass int) {
		switch pass {
		case 1:
			w.tracker.AddBeadChange(application.BeadChange{
				Key: "e1", At: w.at(1), Actor: "mw@laptop", What: "claimed",
				Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusInProgress},
			})
			w.tracker.AddBeadChange(application.BeadChange{
				Key: "c1", At: w.at(2), Actor: "mw@laptop", What: application.ChangeComment,
				Comment: "ANSWER 2026-10-01T13:00:02Z from 02ab, txid direct:abc: yes",
				Bead:    application.BeadNow{ID: "mw-2", Type: "task", Status: application.StatusInProgress, Run: "running"},
			})
		case 2:
			afterChange = kindsOf(w.log.All())
		case 3:
			afterQuiet = kindsOf(w.log.All())
		}
	})
	want := "1:bead_changed:mw-1:open->claimed:status 2:card_answered:mw-2:asked->answered:direct:abc"
	if afterChange != want {
		t.Fatalf("after the changed pass the log holds %q, want %q; said %q", afterChange, want, said)
	}
	if afterQuiet != want {
		t.Fatalf("an unchanged pass appended: the log holds %q, want %q", afterQuiet, want)
	}
	if got := kindsOf(w.log.All()); got != want {
		t.Fatalf("a changed head with no new change appended: the log holds %q, want %q", got, want)
	}
	for _, e := range w.log.All() {
		if e.Actor != "mw@laptop" || e.Lane != events.LaneNormal || e.Ts.IsZero() {
			t.Errorf("event %d: actor %q lane %q ts %v, want mw@laptop, normal and the change's time", e.Seq, e.Actor, e.Lane, e.Ts)
		}
	}
}

func TestTheFirstPassSeedsTheBeadsAndAppendsNothing(t *testing.T) {
	w := newEventsWorld()
	w.tracker.AddBeadChange(application.BeadChange{
		Key: "old", At: w.at(-5), Actor: "mw@laptop", What: "claimed",
		Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusOpen},
	})
	w.run(t, []string{"a", "a"}, nil)
	if got := w.log.All(); len(got) != 0 {
		t.Fatalf("the seeding pass appended %s, want nothing", kindsOf(got))
	}
	if c, ok := w.cursors.Saved(); !ok || c.States["mw-1"] != events.BeadOpen || c.States["mw-2"] != events.BeadRunning || !c.Since.Equal(w.t0) {
		t.Fatalf("the seed saved %+v (%v), want mw-1 open, mw-2 running, since %v", c, ok, w.t0)
	}
}

func TestEachKindOfChangeBecomesItsEvent(t *testing.T) {
	w := newEventsWorld()
	running := application.BeadNow{ID: "mw-2", Type: "task", Status: application.StatusInProgress, Run: "running"}
	comment := func(key string, at int, text string) application.BeadChange {
		return application.BeadChange{Key: key, At: w.at(at), Actor: "mw@laptop", What: application.ChangeComment, Comment: text, Bead: running}
	}
	w.run(t, []string{"a", "b"}, func(pass int) {
		if pass != 1 {
			return
		}
		for _, c := range []application.BeadChange{
			{Key: "e1", At: w.at(1), Actor: "dispatch@laptop", What: "label_added", Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusInProgress, Run: "running"}},
			{Key: "e2", At: w.at(1), Actor: "mw@laptop", What: "created", Bead: application.BeadNow{ID: "mw-m1", Type: "mail", Status: application.StatusOpen, Assignee: "mayor"}},
			{Key: "e3", At: w.at(1), Actor: "mw@laptop", What: "created", Bead: application.BeadNow{ID: "mw-3", Type: "task", Status: application.StatusHeld}},
			{Key: "e4", At: w.at(1), Actor: "mw@laptop", What: "updated", Bead: running},
			comment("c1", 2, "QUESTION 2026-10-01T13:00:02Z asked by postern, txid direct:q1: Ship it? (recommended Yes; options Yes, No)"),
			comment("c2", 3, "RAN step linger on laptop as root, exit 0 (approved by the Governor via postern, txid direct:r1)\n\nok"),
			comment("c3", 4, "The Governor by postern 2026-10-01T13:00:04Z, txid direct:m1, on general:\n\nhello"),
			comment("c4", 5, "Landed by hand."),
		} {
			w.tracker.AddBeadChange(c)
		}
	})
	want := strings.Join([]string{
		"1:bead_changed:mw-1:open->claimed:status",
		"2:bead_changed:mw-1:claimed->running:status",
		"3:mail:mw-m1:->:mayor",
		"4:bead_changed:mw-3:->held:status",
		"5:bead_changed:mw-2:running->running:updated",
		"6:card_asked:mw-2:->asked:direct:q1",
		"7:hands_ran:mw-2:->:linger",
		"8:message:mw-2:->:direct:m1",
		"9:bead_changed:mw-2:running->running:comment",
	}, " ")
	if got := kindsOf(w.log.All()); got != want {
		t.Fatalf("the log holds\n%s\nwant\n%s", strings.ReplaceAll(got, " ", "\n"), strings.ReplaceAll(want, " ", "\n"))
	}
	if got := w.log.All()[0].Actor; got != "dispatch@laptop" {
		t.Errorf("the claim's actor is %q, want the change's, dispatch@laptop", got)
	}
}

func TestAFollowerStartedAgainResumesFromItsCursor(t *testing.T) {
	w := newEventsWorld()
	w.run(t, []string{"a"}, nil)
	w.tracker.AddBeadChange(application.BeadChange{
		Key: "e1", At: w.at(1), Actor: "mw@laptop", What: "closed",
		Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusClosed},
	})
	w.run(t, []string{"b"}, nil)
	w.run(t, []string{"b"}, nil)
	if got, want := kindsOf(w.log.All()), "1:bead_changed:mw-1:open->closed:status"; got != want {
		t.Fatalf("after two restarts the log holds %q, want %q once", got, want)
	}
}

func TestAFailedAppendIsSaidAndTheChangesAreReadAgainNextPass(t *testing.T) {
	w := newEventsWorld()
	w.log.FailNext(errors.New("disk full"))
	w.tracker.AddBeadChange(application.BeadChange{
		Key: "e1", At: w.at(1), Actor: "mw@laptop", What: "closed",
		Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusClosed},
	})
	w.tracker.SetBeadStates(w.t0, application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusOpen})
	said := w.run(t, []string{"a", "b", "b"}, nil)
	if !strings.Contains(said, "disk full") {
		t.Fatalf("expected the failed append said, got %q", said)
	}
	if got, want := kindsOf(w.log.All()), "1:bead_changed:mw-1:open->closed:status"; got != want {
		t.Fatalf("the log holds %q, want the change appended on the retry, %q", got, want)
	}
}

func TestAsideASlowPublishDoesNotHoldUpTheEvents(t *testing.T) {
	w := newEventsWorld()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	release := make(chan struct{})
	var publishes sync.WaitGroup
	publishes.Add(1)
	var once sync.Once
	head := &apptest.FakeHead{Heads: []string{"a", "b"}}
	follow := application.EventFollow{
		Head: head, Feed: w.tracker, Log: w.log, Cursors: w.cursors, Now: w.now,
		Publish: func(ctx context.Context) error {
			once.Do(publishes.Done)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
		Aside: true,
		Sleep: func(ctx context.Context, _ time.Duration) error {
			switch head.Calls() {
			case 1:
				publishes.Wait() // the first view is being built, and stays so
				w.tracker.AddBeadChange(application.BeadChange{
					Key: "e1", At: w.at(1), Actor: "mw@laptop", What: "closed",
					Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusClosed},
				})
				return nil
			default:
				stop()
				return ctx.Err()
			}
		},
	}
	if err := follow.Run(ctx); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got, want := kindsOf(w.log.All()), "1:bead_changed:mw-1:open->closed:status"; got != want {
		t.Fatalf("with the view still being built the log holds %q, want %q", got, want)
	}
}

func TestAChangeStampedAheadDoesNotMoveTheCursorPastTheFollowersClock(t *testing.T) {
	w := newEventsWorld()
	w.clock = w.at(10)
	w.run(t, []string{"a", "b", "c"}, func(pass int) {
		switch pass {
		case 1:
			// A host whose clock runs ten minutes fast.
			w.tracker.AddBeadChange(application.BeadChange{
				Key: "fast", At: w.at(600), Actor: "mw@desktop", What: "claimed",
				Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusInProgress},
			})
		case 2:
			w.tracker.AddBeadChange(application.BeadChange{
				Key: "late", At: w.at(20), Actor: "mw@laptop", What: "closed",
				Bead: application.BeadNow{ID: "mw-2", Type: "task", Status: application.StatusClosed},
			})
		}
	})
	want := "1:bead_changed:mw-1:open->claimed:status 2:bead_changed:mw-2:running->landed:status 3:bead_changed:mw-2:landed->closed:status"
	if got := kindsOf(w.log.All()); got != want {
		t.Fatalf("the log holds %q, want %q: the change after the fast one is not to be lost", got, want)
	}
	if c, _ := w.cursors.Saved(); c.Since.After(w.clock) {
		t.Fatalf("the cursor is at %v, past the follower's clock %v", c.Since, w.clock)
	}
}

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

// mw-gq6.1119: Next.closeStory closed a landed story with a loop of its own,
// PushWait apart on the real clock, and mailed nothing when the close was lost.
// These tests close a landed story through writeRetrying, as mw-gq6.990 did its
// formula: a passing fault waits on RetryWait, an indeterminate close that
// landed is read as closed, and a close lost for good is mailed with its count.

// storyCloseFaults is a tracker whose close of the story fails the first fails
// times with err; with landed, the close that failed was made all the same.
type storyCloseFaults struct {
	*apptest.FakeTracker
	fails  int
	err    error
	landed bool
	asked  int
}

func (s *storyCloseFaults) CloseStory(ctx context.Context, id, reason string) error {
	s.asked++
	if s.asked <= s.fails {
		if s.landed {
			if err := s.FakeTracker.CloseStory(ctx, id, reason); err != nil {
				return err
			}
		}
		return s.err
	}
	return s.FakeTracker.CloseStory(ctx, id, reason)
}

type storyClosed struct {
	report  application.NextReport
	err     error
	tracker *storyCloseFaults
	mail    *apptest.FakeMailbox
	waits   []time.Duration
}

func aLandedStoryWhoseCloseFaults(t *testing.T, fails int, err error, landed bool) *storyClosed {
	t.Helper()
	tracker := &storyCloseFaults{FakeTracker: apptest.NewFakeTracker(), fails: fails, err: err, landed: landed}
	next := aLandingToClose(t, &closeFailsAtFirst{FakeTracker: tracker.FakeTracker})
	next.Tracker = tracker
	run := &storyClosed{tracker: tracker, mail: apptest.NewFakeMailbox()}
	next.Mailbox = run.mail
	next.PushWait = 400 * time.Millisecond
	next.RetryWait = func(_ context.Context, d time.Duration) error {
		run.waits = append(run.waits, d)
		return nil
	}
	run.report, run.err = next.Run(context.Background(), "mw-x.1")
	return run
}

func (r *storyClosed) lostWriteMails(t *testing.T) []application.Message {
	t.Helper()
	inbox, _ := r.mail.Inbox(context.Background(), application.MayorMailbox)
	var lost []application.Message
	for _, m := range inbox {
		if strings.HasPrefix(m.Subject, "lost write") {
			lost = append(lost, m)
		}
	}
	return lost
}

func TestAnIndeterminateStoryCloseThatLandedIsNotWrittenAgainAndMailsNothing(t *testing.T) {
	run := aLandedStoryWhoseCloseFaults(t, 1, errors.New(formulaUnsure), true)
	if run.err != nil || !run.report.Closed {
		t.Fatalf("expected the story closed, got err %v and %+v", run.err, run.report)
	}
	if run.tracker.asked != 1 {
		t.Errorf("expected exactly one close written, got %d", run.tracker.asked)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
}

func TestAStoryCloseThatMeetsAPassingFaultWaitsOnTheRetryWaitAndIsTriedAgain(t *testing.T) {
	run := aLandedStoryWhoseCloseFaults(t, 1, formulaTimeout(), false)
	if run.err != nil || !run.report.Closed {
		t.Fatalf("expected the story closed, got err %v and %+v", run.err, run.report)
	}
	if run.tracker.asked != 2 {
		t.Errorf("expected the close asked twice, got %d", run.tracker.asked)
	}
	if len(run.waits) != 1 {
		t.Errorf("expected one wait on the retry wait before the second try, got %v", run.waits)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
}

func TestAnIndeterminateStoryCloseThatDidNotLandIsTriedAgainAndMailsNothing(t *testing.T) {
	run := aLandedStoryWhoseCloseFaults(t, 1, errors.New(formulaUnsure), false)
	if run.err != nil || !run.report.Closed {
		t.Fatalf("expected the story closed, got err %v and %+v", run.err, run.report)
	}
	if run.tracker.asked != 2 {
		t.Errorf("expected the close asked twice, got %d", run.tracker.asked)
	}
	if mails := run.lostWriteMails(t); len(mails) != 0 {
		t.Errorf("expected no lost-write mail, got %+v", mails)
	}
}

func TestAStoryCloseThatFailsEveryTryMailsALostWriteOnceAndSaysHowManyTimesItWasTried(t *testing.T) {
	run := aLandedStoryWhoseCloseFaults(t, 1000, formulaTimeout(), false)
	if run.err == nil || run.report.Closed {
		t.Fatalf("expected the story landed and still open, got err %v and %+v", run.err, run.report)
	}
	mails := run.lostWriteMails(t)
	if len(mails) != 1 {
		t.Fatalf("expected one lost-write mail, got %+v", mails)
	}
	want := "tried 5 times"
	if !strings.Contains(mails[0].Body, want) || !strings.Contains(mails[0].Body, "i/o timeout") {
		t.Errorf("expected the mail to say it was %q and quote bd, got:\n%s", want, mails[0].Body)
	}
	if run.tracker.asked != application.ReadRetries+1 {
		t.Errorf("expected the close asked %d times, got %d", application.ReadRetries+1, run.tracker.asked)
	}
}

package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1133: the two closes dispatch makes of a story (one found fixed on
// main before it is dispatched, one found landed already by the dead-pane
// reclaim) go through CloseRetry, as Next's closes do: a passing fault is
// waited out and tried again, and a close lost every try is mailed once with
// the try count.

// aDispatchCloseSite is one of the two closes: set builds the dispatch so that
// its close of the story is made, over a tracker whose closes fault as faulter
// says, and closed reads whether the story ends closed.
type aDispatchCloseSite struct {
	name string
	// run sets the site up and runs one dispatch; it returns the tracker the
	// story is read from.
	run func(t *testing.T, faulter *closeFaulter, mail *apptest.FakeMailbox, waits *[]time.Duration) (*apptest.FakeTracker, string)
}

func theDispatchCloseSites() []aDispatchCloseSite {
	return []aDispatchCloseSite{
		{"fixed on main", func(t *testing.T, faulter *closeFaulter, mail *apptest.FakeMailbox, waits *[]time.Duration) (*apptest.FakeTracker, string) {
			dispatch, tracker, _, _, _ := aFactory(t)
			aSuiteBug(t, tracker, "mw-gq6.1", application.LabelBug, application.LabelSuite)
			dispatch.Rechecks = &stubRechecks{answer: application.SpecRecheck{
				Spec: greekSpec, Sha: "9f3c2ab41d0e", Passed: true, Tests: 3, Tail: "  3 passed\n",
			}}
			dispatch.Tracker = &faultingTracker{FakeTracker: tracker, faulter: faulter}
			dispatch.Mailbox, dispatch.PushWait = mail, 400*time.Millisecond
			dispatch.RetryWait = func(_ context.Context, d time.Duration) error { *waits = append(*waits, d); return nil }
			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			return tracker, "mw-gq6.1"
		}},
		{"landed already", func(t *testing.T, faulter *closeFaulter, mail *apptest.FakeMailbox, waits *[]time.Duration) (*apptest.FakeTracker, string) {
			dispatch, tracker, _, _ := aDeadPaneWithALapsedLease(t, &fakeRetryLanding{MergedTip: landedTip})
			dispatch.Tracker = &faultingTracker{FakeTracker: tracker, faulter: faulter}
			dispatch.Mailbox, dispatch.PushWait = mail, 400*time.Millisecond
			dispatch.RetryWait = func(_ context.Context, d time.Duration) error { *waits = append(*waits, d); return nil }
			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("dispatching: %v", err)
			}
			return tracker, "mw-gq6.9"
		}},
	}
}

func lostWrites(t *testing.T, mail *apptest.FakeMailbox) []application.Message {
	t.Helper()
	inbox, _ := mail.Inbox(context.Background(), application.MayorMailbox)
	var lost []application.Message
	for _, m := range inbox {
		if strings.HasPrefix(m.Subject, "lost write") {
			lost = append(lost, m)
		}
	}
	return lost
}

func TestADispatchCloseThatMeetsAPassingFaultWaitsIsTriedAgainAndMailsNothing(t *testing.T) {
	for _, site := range theDispatchCloseSites() {
		t.Run(site.name, func(t *testing.T) {
			faulter := &closeFaulter{fails: 1, err: formulaTimeout()}
			mail := apptest.NewFakeMailbox()
			var waits []time.Duration
			tracker, id := site.run(t, faulter, mail, &waits)
			detail, err := tracker.ShowStory(context.Background(), id)
			if err != nil {
				t.Fatalf("showing %s: %v", id, err)
			}
			if !detail.Closed() {
				t.Errorf("expected %s closed, got %q", id, detail.Status)
			}
			if faulter.asked != 2 {
				t.Errorf("expected the close asked twice, got %d", faulter.asked)
			}
			if len(waits) != 1 {
				t.Errorf("expected one wait before the second try, got %v", waits)
			}
			if mails := lostWrites(t, mail); len(mails) != 0 {
				t.Errorf("expected no lost-write mail, got %+v", mails)
			}
		})
	}
}

func TestADispatchCloseThatFailsEveryTryMailsOnceWithTheTryCount(t *testing.T) {
	for _, site := range theDispatchCloseSites() {
		t.Run(site.name, func(t *testing.T) {
			faulter := &closeFaulter{fails: 1000, err: formulaTimeout()}
			mail := apptest.NewFakeMailbox()
			var waits []time.Duration
			tracker, id := site.run(t, faulter, mail, &waits)
			detail, _ := tracker.ShowStory(context.Background(), id)
			if detail.Closed() {
				t.Errorf("expected %s still open", id)
			}
			if faulter.asked != application.ReadRetries+1 {
				t.Errorf("expected the close asked %d times, got %d", application.ReadRetries+1, faulter.asked)
			}
			mails := lostWrites(t, mail)
			if len(mails) != 1 {
				t.Fatalf("expected one lost-write mail, got %+v", mails)
			}
			if want := "tried 5 times"; !strings.Contains(mails[0].Body, want) || !strings.Contains(mails[0].Body, id) {
				t.Errorf("expected the mail to name %s and say %q, got:\n%s", id, want, mails[0].Body)
			}
		})
	}
}

func TestADispatchCloseThatLandedButReportedIndeterminateIsNotWrittenAgain(t *testing.T) {
	for _, site := range theDispatchCloseSites() {
		t.Run(site.name, func(t *testing.T) {
			faulter := &closeFaulter{fails: 1, err: errIndeterminate(), landed: true}
			mail := apptest.NewFakeMailbox()
			var waits []time.Duration
			tracker, id := site.run(t, faulter, mail, &waits)
			detail, _ := tracker.ShowStory(context.Background(), id)
			if !detail.Closed() || faulter.asked != 1 {
				t.Errorf("expected %s closed by one close, got closed %v after %d", id, detail.Closed(), faulter.asked)
			}
			if mails := lostWrites(t, mail); len(mails) != 0 {
				t.Errorf("expected no lost-write mail, got %+v", mails)
			}
		})
	}
}

func errIndeterminate() error { return errString(formulaUnsure) }

type errString string

func (e errString) Error() string { return string(e) }

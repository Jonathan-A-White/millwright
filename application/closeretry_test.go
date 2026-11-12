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
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1131: Next's close retry (read first on an indeterminate commit, wait
// out a passing fault, mail a close lost for good with its try count) is one
// helper, CloseRetry, that a caller without a Next uses. These tests close
// through it directly and through one close path of backendauto.go, triage.go
// and verifyconfirm.go.

// closeFaulter makes the first fails closes of a bead fail with err; with
// landed, the close that failed was made all the same.
type closeFaulter struct {
	fails  int
	err    error
	landed bool
	asked  int
}

// close is one close that may fault; real makes it for good.
func (f *closeFaulter) close(real func() error) error {
	f.asked++
	if f.asked <= f.fails {
		if f.landed {
			if err := real(); err != nil {
				return err
			}
		}
		return f.err
	}
	return real()
}

// closeRetryRun is what one of the cases below ran against.
type closeRetryRun struct {
	faulter *closeFaulter
	mail    *apptest.FakeMailbox
	waits   []time.Duration
}

func aCloseRun(fails int, err error, landed bool) *closeRetryRun {
	return &closeRetryRun{faulter: &closeFaulter{fails: fails, err: err, landed: landed}, mail: apptest.NewFakeMailbox()}
}

func (r *closeRetryRun) wait(_ context.Context, d time.Duration) error {
	r.waits = append(r.waits, d)
	return nil
}

func (r *closeRetryRun) lostWriteMails(t *testing.T) []application.Message {
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

// The three things AC3 asks of every close path, run once for each of them.
type closePath struct {
	name string
	// run closes one story whose close faults as the run says, and reports the
	// error of the close and whether the story reads closed after it.
	run func(t *testing.T, r *closeRetryRun) (err error, closed bool)
}

func theClosePaths() []closePath {
	return []closePath{
		{"the helper", func(t *testing.T, r *closeRetryRun) (error, bool) {
			tracker := apptest.NewFakeTracker()
			tracker.AddEpic("e-1", domain.Path{})
			tracker.AddStory("e-1", domain.Story{ID: "e-1.1", Title: "A story"})
			c := application.CloseRetry{
				Close: func(ctx context.Context, id, reason string) error {
					return r.faulter.close(func() error { return tracker.CloseStory(ctx, id, reason) })
				},
				Show:      tracker.ShowStory,
				Host:      "cloud2",
				PushWait:  400 * time.Millisecond,
				RetryWait: r.wait,
				Mailbox:   r.mail,
			}
			err := c.CloseStory(context.Background(), "mw test", "closing", "e-1.1", "done")
			detail, _ := tracker.ShowStory(context.Background(), "e-1.1")
			return err, detail.Closed()
		}},
		{"backendauto.go", func(t *testing.T, r *closeRetryRun) (error, bool) {
			a := anAutoRig(t, application.HandsOutcome{Exit: 0, Output: swapAnswered})
			a.stage.Landed(context.Background(), presenceLanding())
			bead := a.stagedBead(t)
			a.stage.Tracker = &faultingTracker{FakeTracker: a.tracker, faulter: r.faulter}
			a.stage.Mailbox, a.stage.PushWait, a.stage.RetryWait = r.mail, 400*time.Millisecond, r.wait
			lines := a.stage.Pending(context.Background())
			var err error
			for _, l := range lines {
				if strings.Contains(l, "closing "+bead) {
					err = errors.New(l)
				}
			}
			return err, a.bead(t, bead).Status == application.StatusClosed
		}},
		{"triage.go", func(t *testing.T, r *closeRetryRun) (error, bool) {
			fake := orphanFixture()
			sweep := application.TriageOrphans{
				Source: fake, Closer: &faultingTriage{FakeTriage: fake, faulter: r.faulter},
				Apply: true, Host: "cloud2", PushWait: 400 * time.Millisecond, RetryWait: r.wait, Mailbox: r.mail,
			}
			_, err := sweep.Run(context.Background())
			closed := false
			for _, id := range fake.Closed() {
				closed = closed || id == "mw-mol-a1"
			}
			return err, closed
		}},
		{"verifyconfirm.go", func(t *testing.T, r *closeRetryRun) (error, bool) {
			rig := newConfirmRig(t)
			confirm := rig.confirm()
			confirm.Beads = &faultingVerify{FakeVerifyBeads: rig.beads, faulter: r.faulter}
			confirm.Host, confirm.PushWait, confirm.RetryWait, confirm.Mailbox = "cloud2", 400*time.Millisecond, r.wait, r.mail
			var out bytes.Buffer
			confirm.Out = &out
			err := confirm.Run(context.Background(), application.VerifyConfirmRequest{Bead: "d-1", Txid: "direct:aaa", As: "mayor"})
			details, _ := rig.beads.ShowBeads(context.Background(), []string{"d-1"})
			return err, len(details) == 1 && details[0].Closed()
		}},
	}
}

type faultingTracker struct {
	*apptest.FakeTracker
	faulter *closeFaulter
}

func (f *faultingTracker) CloseStory(ctx context.Context, id, reason string) error {
	return f.faulter.close(func() error { return f.FakeTracker.CloseStory(ctx, id, reason) })
}

type faultingTriage struct {
	*apptest.FakeTriage
	faulter *closeFaulter
}

// CloseStory faults the close of the first orphan only; the second is closed as it is asked.
func (f *faultingTriage) CloseStory(ctx context.Context, id, reason string) error {
	if id != "mw-mol-a1" {
		return f.FakeTriage.CloseStory(ctx, id, reason)
	}
	return f.faulter.close(func() error { return f.FakeTriage.CloseStory(ctx, id, reason) })
}

type faultingVerify struct {
	*apptest.FakeVerifyBeads
	faulter *closeFaulter
}

func (f *faultingVerify) CloseStory(ctx context.Context, id, reason string) error {
	return f.faulter.close(func() error { return f.FakeVerifyBeads.CloseStory(ctx, id, reason) })
}

func TestACloseThatMeetsAPassingFaultWaitsAndIsTriedAgainAndMailsNothing(t *testing.T) {
	for _, p := range theClosePaths() {
		t.Run(p.name, func(t *testing.T) {
			r := aCloseRun(1, formulaTimeout(), false)
			err, closed := p.run(t, r)
			if err != nil || !closed {
				t.Fatalf("expected the story closed, got err %v, closed %v", err, closed)
			}
			if r.faulter.asked != 2 {
				t.Errorf("expected the close asked twice, got %d", r.faulter.asked)
			}
			if len(r.waits) != 1 {
				t.Errorf("expected one wait on the retry wait, got %v", r.waits)
			}
			if mails := r.lostWriteMails(t); len(mails) != 0 {
				t.Errorf("expected no lost-write mail, got %+v", mails)
			}
		})
	}
}

func TestAnIndeterminateCloseThatLandedIsReadAsClosedAndNotWrittenAgain(t *testing.T) {
	for _, p := range theClosePaths() {
		t.Run(p.name, func(t *testing.T) {
			r := aCloseRun(1, errors.New(formulaUnsure), true)
			err, closed := p.run(t, r)
			if err != nil || !closed {
				t.Fatalf("expected the story closed, got err %v, closed %v", err, closed)
			}
			if r.faulter.asked != 1 {
				t.Errorf("expected exactly one close written, got %d", r.faulter.asked)
			}
			if mails := r.lostWriteMails(t); len(mails) != 0 {
				t.Errorf("expected no lost-write mail, got %+v", mails)
			}
		})
	}
}

func TestACloseThatFailsEveryTryMailsOnceWithTheTryCount(t *testing.T) {
	for _, p := range theClosePaths() {
		t.Run(p.name, func(t *testing.T) {
			r := aCloseRun(1000, formulaTimeout(), false)
			err, closed := p.run(t, r)
			if err == nil || closed {
				t.Fatalf("expected the close lost, got err %v, closed %v", err, closed)
			}
			mails := r.lostWriteMails(t)
			if len(mails) != 1 {
				t.Fatalf("expected one lost-write mail, got %+v", mails)
			}
			if want := "tried 5 times"; !strings.Contains(mails[0].Body, want) || !strings.Contains(mails[0].Body, "i/o timeout") {
				t.Errorf("expected the mail to say %q and quote bd, got:\n%s", want, mails[0].Body)
			}
			if r.faulter.asked != application.ReadRetries+1 {
				t.Errorf("expected the close asked %d times, got %d", application.ReadRetries+1, r.faulter.asked)
			}
		})
	}
}

package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1454: a batch's gate failed and its failing tests were not run alone on
// main, so the batch's mail to the Mayor opens with "main not checked", as a
// refused story's does (mw-gq6.1421), and not with the gate's failure alone.

// aBatchHeldAfterMainAlone lands a batch of two whose gate fails TestA and
// TestB, with no fixer, the run of those tests on main alone being named, and
// returns the Held mail the Mayor was sent.
func aBatchHeldAfterMainAlone(t *testing.T, named application.NamedChecks) (*application.BatchReport, string) {
	t.Helper()
	a := aBatchOf(t, 2)
	a.land.Fix = false
	a.land.Next.NamedChecks = named
	a.land.Next.Checks = twoFailingTests{}
	a.land.Next.Slot = aSlot{}
	a.land.Next.Rigs = map[string]string{"millwright": "/rigs/millwright"}
	a.land.Next.MainAloneBound = 10 * time.Minute
	a.land.Next.LoadWait = func(context.Context, time.Duration) error { return nil }
	mailbox := apptest.NewFakeMailbox()
	a.land.Next.Mailbox = mailbox
	a.land.Next.Out, a.land.Next.Err = io.Discard, io.Discard

	a.run(t)
	if a.report.Landed || a.report.Held == "" {
		t.Fatalf("expected the batch held for its red gate, got landed %v, held %q", a.report.Landed, a.report.Held)
	}
	a.land.MailTheBatch(context.Background(), a.report)

	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected one mail to the Mayor, got %d", len(inbox))
	}
	return a.report, inbox[0].Body
}

func TestABatchMailOpensWithMainNotChecked(t *testing.T) {
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{}, &application.RunCutError{Bound: 10 * time.Minute}
	}}
	report, mail := aBatchHeldAfterMainAlone(t, named)
	if len(named.asked) != 1 {
		t.Fatalf("expected the failing tests asked of main once, got %q", named.asked)
	}
	if report.MainNotChecked == "" {
		t.Errorf("expected the report to say why main was not checked")
	}
	first := strings.SplitN(mail, "\n", 2)[0]
	if !strings.Contains(first, application.MainNotCheckedWords) || !strings.Contains(first, "did not end in") {
		t.Errorf("expected the first line of the mail to say main was not checked, got %q (mail: %s)", first, mail)
	}
}

func TestABatchMailAfterARunOfMainThatWasMadeDoesNotSayMainWasNotChecked(t *testing.T) {
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{Checked: application.Checked{Command: "go test -run", Passed: true}, Ran: true}, nil
	}}
	_, mail := aBatchHeldAfterMainAlone(t, named)
	if strings.Contains(mail, application.MainNotCheckedWords) {
		t.Errorf("expected no main not checked after a run on main that was made, got:\n%s", mail)
	}
}

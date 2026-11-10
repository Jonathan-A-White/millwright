package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// failingSync is a HostSync that always fails with err.
type failingSync struct{ err error }

func (s failingSync) Run(context.Context) (application.SyncReport, error) {
	return application.SyncReport{Host: "vps"}, s.err
}

// anAlarmDispatch is a dispatch with a tick log and a mailbox whose sync fails
// with err every tick.
func anAlarmDispatch(t *testing.T, err error) (application.Dispatch, *apptest.FakeMailbox) {
	t.Helper()
	dispatch, _, _, _, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	dispatch.Sync = failingSync{err: err}
	dispatch.Log = &apptest.FakeTickLog{}
	dispatch.Mailbox = mailbox
	return dispatch, mailbox
}

func mayorMail(t *testing.T, mailbox *apptest.FakeMailbox) []application.Message {
	t.Helper()
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatalf("reading the Mayor's inbox: %v", err)
	}
	return inbox
}

func TestTheSkippedTickThatRaisesTheAlarmMailsTheMayorOnce(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox := anAlarmDispatch(t, &application.RemoteUnreachable{Remote: "github.com", Said: "Connection timed out"})

	for tick := 1; tick < application.SkippedTicksAlarm; tick++ {
		if _, err := dispatch.Run(ctx); err != nil {
			t.Fatalf("skipped tick %d failed: %v", tick, err)
		}
		if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
			t.Fatalf("the tick before the alarm (tick %d) sent %d mails, want none", tick, len(inbox))
		}
	}

	_, err := dispatch.Run(ctx)
	if err == nil {
		t.Fatalf("the alarm tick did not fail the unit")
	}
	inbox := mayorMail(t, mailbox)
	if len(inbox) != 1 {
		t.Fatalf("the alarm tick sent %d mails to the Mayor, want 1", len(inbox))
	}
	if !strings.Contains(inbox[0].Subject, "vps") || !strings.Contains(inbox[0].Subject, "github.com") {
		t.Fatalf("subject %q does not name the host and the remote", inbox[0].Subject)
	}
	if !strings.Contains(inbox[0].Body, err.Error()) || !strings.Contains(inbox[0].Body, "mw status") {
		t.Fatalf("body %q lacks the alarm text or where mw status shows it", inbox[0].Body)
	}

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("the tick after the alarm failed: %v", err)
	}
	if inbox := mayorMail(t, mailbox); len(inbox) != 1 {
		t.Fatalf("the tick after the alarm sent another mail: %d in all, want 1", len(inbox))
	}
}

func TestTheFaultedTickThatRaisesTheAlarmMailsTheMayorOnce(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox := anAlarmDispatch(t, &application.NameNotResolved{Said: "Could not resolve host: github.com"})

	for tick := 1; tick < application.FaultedTicksAlarm; tick++ {
		_, err := dispatch.Run(ctx)
		if _, faulted := application.LocalFault(err); !faulted {
			t.Fatalf("faulted tick %d gave %v, want the local network fault", tick, err)
		}
		if inbox := mayorMail(t, mailbox); len(inbox) != 0 {
			t.Fatalf("the tick before the alarm (tick %d) sent %d mails, want none", tick, len(inbox))
		}
	}

	_, err := dispatch.Run(ctx)
	if err == nil {
		t.Fatalf("the alarm tick did not fail the unit")
	}
	if _, faulted := application.LocalFault(err); faulted {
		t.Fatalf("the alarm tick is still the plain fault, want a failure: %v", err)
	}
	inbox := mayorMail(t, mailbox)
	if len(inbox) != 1 {
		t.Fatalf("the alarm tick sent %d mails to the Mayor, want 1", len(inbox))
	}
	if !strings.Contains(inbox[0].Subject, "vps") || !strings.Contains(inbox[0].Subject, "network") {
		t.Fatalf("subject %q does not name the host and the fault", inbox[0].Subject)
	}
	if !strings.Contains(inbox[0].Body, err.Error()) || !strings.Contains(inbox[0].Body, "mw status") {
		t.Fatalf("body %q lacks the alarm text or where mw status shows it", inbox[0].Body)
	}

	_, err = dispatch.Run(ctx)
	if _, faulted := application.LocalFault(err); !faulted {
		t.Fatalf("the tick after the alarm gave %v, want the plain fault again", err)
	}
	if inbox := mayorMail(t, mailbox); len(inbox) != 1 {
		t.Fatalf("the tick after the alarm sent another mail: %d in all, want 1", len(inbox))
	}
}

func TestAnAlarmTickWithNoMailboxSendsNothingAndStillFails(t *testing.T) {
	ctx := context.Background()
	for name, cause := range map[string]error{
		"skipped": &application.RemoteUnreachable{Remote: "github.com", Said: "Connection timed out"},
		"faulted": &application.NameNotResolved{Said: "Could not resolve host: github.com"},
	} {
		dispatch, _ := anAlarmDispatch(t, cause)
		dispatch.Mailbox = nil
		var err error
		for tick := 1; tick <= application.SkippedTicksAlarm; tick++ {
			_, err = dispatch.Run(ctx)
		}
		if err == nil {
			t.Fatalf("%s: the alarm tick did not fail the unit", name)
		}
		if _, faulted := application.LocalFault(err); faulted {
			t.Fatalf("%s: the alarm tick is still the plain fault: %v", name, err)
		}
	}
}

func TestAnAlarmMailThatCannotBeSentIsANoteInTheReport(t *testing.T) {
	ctx := context.Background()
	for name, cause := range map[string]error{
		"skipped": &application.RemoteUnreachable{Remote: "github.com", Said: "Connection timed out"},
		"faulted": &application.NameNotResolved{Said: "Could not resolve host: github.com"},
	} {
		dispatch, mailbox := anAlarmDispatch(t, cause)
		mailbox.Err = errors.New("postern is down")
		var report application.DispatchReport
		var err error
		for tick := 1; tick <= application.SkippedTicksAlarm; tick++ {
			report, err = dispatch.Run(ctx)
		}
		if err == nil {
			t.Fatalf("%s: the alarm tick did not fail the unit", name)
		}
		found := false
		for _, note := range report.Notes {
			if strings.Contains(note, "could not be sent") && strings.Contains(note, "postern is down") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: notes %q hold no note that the mail could not be sent", name, report.Notes)
		}
	}
}

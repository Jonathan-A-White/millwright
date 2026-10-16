package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aMayorBoxOf is a mailbox holding n unread messages for the mayor, subjects
// "mail 1" to "mail n", oldest first.
func aMayorBoxOf(t *testing.T, n int) *apptest.FakeMailbox {
	t.Helper()
	box := apptest.NewFakeMailbox()
	for i := 1; i <= n; i++ {
		if _, err := box.Send(context.Background(), application.NewMessage{
			From: "deputy", To: "mayor", Subject: fmt.Sprintf("mail %d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return box
}

// inboxLines lists the mayor's inbox, all of it or not, and returns the lines
// it printed and the messages it reported.
func inboxLines(t *testing.T, box *apptest.FakeMailbox, all bool) ([]string, []application.Message) {
	t.Helper()
	var out strings.Builder
	unread, err := (application.Mail{Mailbox: box, Seat: "mayor", Out: &out}).Inbox(context.Background(), "", all)
	if err != nil {
		t.Fatalf("listing the inbox: %v", err)
	}
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n"), unread
}

// Seen by Mayor 395 (mw-gq6.443): a box of a thousand and more unread printed
// every one of them oldest first, and the newest mail fell off the end of a
// screen nobody reads to the bottom of. The inbox shows the newest fifty, newest
// last, under a line that names how many older ones it left out.
func TestTheInboxOfABigBoxShowsTheNewestFiftyLastAndNamesTheOlder(t *testing.T) {
	lines, unread := inboxLines(t, aMayorBoxOf(t, 1200), false)

	if len(lines) != application.InboxShown+1 {
		t.Fatalf("expected %d lines, the newest %d and one naming the older, got %d; the last: %q",
			application.InboxShown+1, application.InboxShown, len(lines), lines[len(lines)-1])
	}
	if want := "1150 older unread not shown; --all to list them"; lines[0] != want {
		t.Errorf("expected the first line to be %q, got %q", want, lines[0])
	}
	if !strings.HasSuffix(lines[1], "  mail 1151") {
		t.Errorf("expected the oldest message shown to be mail 1151, got %q", lines[1])
	}
	if !strings.HasSuffix(lines[len(lines)-1], "  mail 1200") {
		t.Errorf("expected the newest message, mail 1200, last, got %q", lines[len(lines)-1])
	}
	if len(unread) != 1200 {
		t.Errorf("expected the inbox to report all 1200 unread, printed or not, got %d", len(unread))
	}
}

func TestTheInboxListsEveryUnreadMessageWithAll(t *testing.T) {
	lines, _ := inboxLines(t, aMayorBoxOf(t, 1200), true)

	if len(lines) != 1200 {
		t.Fatalf("expected all 1200 messages listed, got %d lines; the first: %q", len(lines), lines[0])
	}
	if !strings.HasSuffix(lines[0], "  mail 1") || !strings.HasSuffix(lines[1199], "  mail 1200") {
		t.Errorf("expected mail 1 first and mail 1200 last, got %q and %q", lines[0], lines[1199])
	}
	if strings.Contains(strings.Join(lines, "\n"), "not shown") {
		t.Errorf("expected no line about mail not shown when all are listed")
	}
}

func TestAnInboxOfNoMoreThanFiftyIsListedWholeWithNoWordOfOlderMail(t *testing.T) {
	lines, _ := inboxLines(t, aMayorBoxOf(t, application.InboxShown), false)

	if len(lines) != application.InboxShown || !strings.HasSuffix(lines[0], "  mail 1") {
		t.Fatalf("expected all %d messages from mail 1, got %d lines; the first: %q", application.InboxShown, len(lines), lines[0])
	}
}

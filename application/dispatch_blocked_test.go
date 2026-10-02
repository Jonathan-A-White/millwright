package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// blockedSync is a HostSync that refuses on a vault holding uncommitted files.
type blockedSync struct{ files []string }

func (s *blockedSync) Run(context.Context) (application.SyncReport, error) {
	return application.SyncReport{Host: "vps"}, &application.VaultBlocked{Host: "vps", Files: s.files}
}

// aBlockedDispatch is a dispatch whose sync refuses on a dirty vault, with a
// mailbox, a note store and a clock the test moves.
func aBlockedDispatch(t *testing.T, files ...string) (application.Dispatch, *apptest.FakeMailbox, *time.Time) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	mailbox := apptest.NewFakeMailbox()
	now := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	dispatch.Sync = &blockedSync{files: files}
	dispatch.Mailbox = mailbox
	dispatch.Memory = tracker
	dispatch.Now = func() time.Time { return now }
	return dispatch, mailbox, &now
}

func TestDispatchMailsTheMayorOnceAnHourWhenTheVaultIsDirty(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox, now := aBlockedDispatch(t, ".mayor-up.log")

	if _, err := dispatch.Run(ctx); err == nil {
		t.Fatalf("expected a dispatch on a dirty vault to be refused")
	}
	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 {
		t.Fatalf("expected one mail to the mayor, got %d", len(inbox))
	}
	if !strings.Contains(inbox[0].Body, ".mayor-up.log") || !strings.Contains(inbox[0].Subject, "vps") {
		t.Errorf("expected the mail to name the host and the dirty file, got %q / %q", inbox[0].Subject, inbox[0].Body)
	}

	*now = now.Add(5 * time.Minute)
	if _, err := dispatch.Run(ctx); err == nil {
		t.Fatalf("expected the second dispatch to be refused too")
	}
	if inbox, _ = mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 1 {
		t.Errorf("expected the same reason within the hour to send nothing new, got %d mails", len(inbox))
	}

	*now = now.Add(time.Hour)
	_, _ = dispatch.Run(ctx)
	if inbox, _ = mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 2 {
		t.Errorf("expected the same reason an hour on to be told again, got %d mails", len(inbox))
	}
}

func TestDispatchMailsAgainAtOnceForADifferentDirtyFile(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox, now := aBlockedDispatch(t, ".mayor-up.log")
	_, _ = dispatch.Run(ctx)

	*now = now.Add(time.Minute)
	dispatch.Sync = &blockedSync{files: []string{"CHARTER.md"}}
	_, _ = dispatch.Run(ctx)

	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 2 || !strings.Contains(inbox[1].Body, "CHARTER.md") {
		t.Errorf("expected a second mail naming CHARTER.md, got %+v", inbox)
	}
}

func TestDispatchKeepsQuietOnADirtyVaultWithoutAMailboxOrMemory(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox, _ := aBlockedDispatch(t, ".mayor-up.log")
	dispatch.Memory = nil
	if _, err := dispatch.Run(ctx); err == nil {
		t.Fatalf("expected a refusal")
	}
	if mailbox.Writes() != 0 {
		t.Errorf("expected no mail without a memory to keep the hour in, got %d writes", mailbox.Writes())
	}
}

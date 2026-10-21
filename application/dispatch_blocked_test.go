package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// blockedSync is a HostSync that refuses on a vault holding uncommitted files.
type blockedSync struct{ files []string }

func (s *blockedSync) Run(context.Context) (application.SyncReport, error) {
	return application.SyncReport{Host: "vps"}, &application.VaultBlocked{Host: "vps", Files: s.files}
}

// cleanSync is a HostSync that finds the vault clean and the hosts level.
type cleanSync struct{}

func (cleanSync) Run(context.Context) (application.SyncReport, error) {
	return application.SyncReport{Host: "vps"}, nil
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
	if mailbox.Writes() != 0 {
		t.Fatalf("expected the first sighting to send nothing, got %d writes", mailbox.Writes())
	}
	*now = now.Add(time.Minute)
	_, _ = dispatch.Run(ctx)
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
	_, _ = dispatch.Run(ctx)

	*now = now.Add(time.Minute)
	dispatch.Sync = &blockedSync{files: []string{"CHARTER.md"}}
	_, _ = dispatch.Run(ctx)
	if inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox); len(inbox) != 1 {
		t.Fatalf("expected a different file set to wait for its second sighting, got %d mails", len(inbox))
	}
	*now = now.Add(time.Minute)
	_, _ = dispatch.Run(ctx)

	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 2 || !strings.Contains(inbox[1].Body, "CHARTER.md") {
		t.Errorf("expected a second mail naming CHARTER.md, got %+v", inbox)
	}
}

func TestDispatchSendsNoMailWhenTheVaultIsCleanAtTheNextTick(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox, now := aBlockedDispatch(t, "runs/mw-gq6.230/result.json")
	_, _ = dispatch.Run(ctx)

	// the close-out committed its files: the next tick finds the vault clean
	*now = now.Add(time.Minute)
	dispatch.Sync = cleanSync{}
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("expected a clean tick to dispatch, got %v", err)
	}
	// the same files dirty again are a first sighting once more, not a second
	*now = now.Add(time.Minute)
	dispatch.Sync = &blockedSync{files: []string{"runs/mw-gq6.230/result.json"}}
	_, _ = dispatch.Run(ctx)

	if mailbox.Writes() != 0 {
		inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
		t.Errorf("expected no mail when the vault was clean between two sightings, got %d writes: %+v", mailbox.Writes(), inbox)
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

// The mail names each file and how to clear it: who writes it when mw knows,
// and the one command that commits it as found (mw-gq6.462).
func TestTheDirtyVaultMailNamesTheFileAndTheFix(t *testing.T) {
	ctx := context.Background()
	dispatch, mailbox, now := aBlockedDispatch(t, "hosts/vultr/cloud.json")
	dispatch.VaultDir = "/home/jwhite/millwright-vault"
	_, _ = dispatch.Run(ctx)
	*now = now.Add(time.Minute)
	_, _ = dispatch.Run(ctx)

	inbox, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	if len(inbox) != 1 {
		t.Fatalf("expected one mail to the mayor, got %d", len(inbox))
	}
	body := inbox[0].Body
	for _, want := range []string{
		"hosts/vultr/cloud.json is the cloud's book, written and committed by mw cloud check",
		"git -C /home/jwhite/millwright-vault commit -m 'commit as found what dispatch on vps was refused on' -- hosts/vultr/cloud.json",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected the mail to say %q, got:\n%s", want, body)
		}
	}
}

func TestTheDirtyVaultFixQuotesWhatTheShellWouldSplit(t *testing.T) {
	got := application.VaultBlockedFix("/home/j w/vault", "vps", []string{"seats/mayor/ledger.md", "plans/a b.md"})
	want := "git -C '/home/j w/vault' commit -m 'commit as found what dispatch on vps was refused on' -- seats/mayor/ledger.md 'plans/a b.md'"
	if got != want {
		t.Errorf("expected\n  %s\ngot\n  %s", want, got)
	}
}

// A vault push GitHub refused through every retry (other pushers landing in the
// same seconds) is a tick that did not get its sync, not a unit that failed:
// the tick warns naming the attempts, claims nothing and ends with no error;
// the next one goes (mw-gq6.689).
func TestDispatchWarnsAndEndsCleanWhenTheVaultPushIsStillRefusedAfterTheRetries(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	refused := fmt.Errorf("syncing the vault on vps: %w", &application.VaultPushRaced{
		Attempts: 4, Err: errors.New("git push: [remote rejected] main -> main (cannot lock ref 'refs/heads/main')"),
	})
	dispatch.Sync = &stubSync{err: refused}
	var out strings.Builder
	dispatch.Out = &out

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("expected a raced push not to fail the tick, got %v", err)
	}
	if len(report.Started) != 0 {
		t.Fatalf("expected nothing started, got %v", startedIDs(report))
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.1"); detail.Assignee != "" {
		t.Fatalf("the story was claimed by %q", detail.Assignee)
	}
	if said := out.String(); !strings.Contains(said, "WARN") || !strings.Contains(said, "4 pushes") {
		t.Fatalf("expected a WARN line naming the attempts, got %q", said)
	}
}

// A sync that failed for a reason a wait does not cure still fails the tick.
func TestDispatchStillFailsOnAVaultPushRefusedForAnotherReason(t *testing.T) {
	dispatch, _, _, _, _ := aFactory(t)
	dispatch.Sync = &stubSync{err: fmt.Errorf("syncing the vault on vps: git push: Permission denied (publickey)")}

	if _, err := dispatch.Run(context.Background()); err == nil {
		t.Fatal("expected an authentication failure to fail the tick")
	}
}

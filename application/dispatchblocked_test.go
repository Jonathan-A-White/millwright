package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aDamagedVaultDispatch is a dispatch whose sync is blocked on files that a
// damaged repository lists, mailed on the second tick as a dirty vault is.
func aDamagedVaultDispatch(t *testing.T, damage application.VaultDamage, files ...string) application.Dispatch {
	t.Helper()
	dispatch, mailbox, now := aBlockedDispatch(t, files...)
	dispatch.VaultDir = "/home/mw/millwright-vault"
	dispatch.Sync = &blockedSync{files: files, damage: damage}
	ctx := context.Background()
	_, _ = dispatch.Run(ctx)
	*now = now.Add(time.Minute)
	_, _ = dispatch.Run(ctx)
	if mailbox.Writes() != 1 {
		t.Fatalf("expected one mail to the mayor, got %d", mailbox.Writes())
	}
	dispatch.Mailbox = mailbox
	return dispatch
}

func mailToTheMayor(t *testing.T, d application.Dispatch) application.Message {
	t.Helper()
	inbox, err := d.Mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil || len(inbox) != 1 {
		t.Fatalf("expected one mail to the mayor, got %d (%v)", len(inbox), err)
	}
	return inbox[0]
}

// mw-gq6.1167: a corrupt pack made 816 files look uncommitted, and the mail told
// the Mayor to commit them.
func TestACorruptVaultIsMailedAsCorruptAndNamesTheRefetch(t *testing.T) {
	damage := application.VaultDamage{Corrupt: "error: packed object c6bf6432 is corrupt"}
	mail := mailToTheMayor(t, aDamagedVaultDispatch(t, damage, "seats/mayor/ledger.md", "hosts/vultr/cloud.json"))

	said := mail.Subject + "\n" + mail.Body
	if !strings.Contains(said, "corrupt") {
		t.Errorf("expected the mail to say the vault's git repository is corrupt, got %q", said)
	}
	if !strings.Contains(said, "fetch --refetch") {
		t.Errorf("expected the mail to name the refetch, got %q", said)
	}
	if strings.Contains(mail.Subject, "uncommitted changes") {
		t.Errorf("expected the subject not to call a corrupt repository uncommitted work, got %q", mail.Subject)
	}
	if strings.Contains(said, "commit them as found") || strings.Contains(said, "git -C /home/mw/millwright-vault commit") {
		t.Errorf("expected the mail not to tell the Mayor to commit what the corruption lists, got %q", said)
	}
	if !strings.Contains(said, "packed object c6bf6432 is corrupt") {
		t.Errorf("expected git's own words in the mail, got %q", said)
	}
}

func TestAVaultStoppedMidRebaseIsMailedAsSuchAndNamesTheAbort(t *testing.T) {
	mail := mailToTheMayor(t, aDamagedVaultDispatch(t, application.VaultDamage{Rebasing: true}, "seats/mayor/ledger.md"))

	said := mail.Subject + "\n" + mail.Body
	if !strings.Contains(said, "rebase") {
		t.Errorf("expected the mail to say a rebase stopped, got %q", said)
	}
	if !strings.Contains(said, "git -C /home/mw/millwright-vault rebase --abort") {
		t.Errorf("expected the mail to name the command that ends the rebase, got %q", said)
	}
	if strings.Contains(mail.Subject, "uncommitted changes") {
		t.Errorf("expected the subject not to say uncommitted changes, got %q", mail.Subject)
	}
}

// A damaged vault is not the case of files no dispatch reads: the tick stops.
func TestADamagedVaultOfHelperScriptsStillStopsTheDispatch(t *testing.T) {
	dispatch, _, _ := aBlockedDispatch(t, "bin/helper.sh")
	dispatch.Sync = &blockedSync{files: []string{"bin/helper.sh"}, damage: application.VaultDamage{Corrupt: "bad pack"}}
	if _, err := dispatch.Run(context.Background()); err == nil {
		t.Fatalf("expected a corrupt vault to refuse the dispatch even when only helper scripts are listed")
	}
}

func TestTheRefusalNamesTheDamageNotUncommittedWork(t *testing.T) {
	corrupt := &application.VaultBlocked{Host: "cloud4", Files: []string{"a.md"}, Damage: application.VaultDamage{Corrupt: "bad pack"}}
	if got := corrupt.Error(); !strings.Contains(got, "corrupt") || !strings.Contains(got, "fetch --refetch") || strings.Contains(got, "commit them when you can") {
		t.Errorf("expected a corrupt-repository sentence, got %q", got)
	}
	rebasing := &application.VaultBlocked{Host: "cloud4", Files: []string{"a.md"}, Damage: application.VaultDamage{Rebasing: true}}
	if got := rebasing.Error(); !strings.Contains(got, "rebase --abort") {
		t.Errorf("expected the rebase to be named, got %q", got)
	}
	plain := &application.VaultBlocked{Host: "cloud4", Files: []string{"a.md"}}
	if got := plain.Error(); !strings.Contains(got, "uncommitted changes to a.md") {
		t.Errorf("expected plain uncommitted work to read as before, got %q", got)
	}
	if got := (application.SyncReport{Host: "cloud4", Blocked: []string{"a.md"}, Damage: application.VaultDamage{Corrupt: "x"}}).String(); !strings.Contains(got, "corrupt") {
		t.Errorf("expected the sync's one line to say corrupt, got %q", got)
	}
}

func TestASyncOnABlockedVaultReadsTheDamageOnce(t *testing.T) {
	sync, files, _ := syncing(t)
	files.Dirty = []string{"a.md"}
	files.Damaged = application.VaultDamage{Rebasing: true}

	report, err := sync.Run(context.Background())
	blocked, ok := application.Blocked(err)
	if !ok {
		t.Fatalf("expected the vault to be blocked, got %v", err)
	}
	if !blocked.Damage.Rebasing || !report.Damage.Rebasing {
		t.Errorf("expected the damage on the refusal and the report, got %+v / %+v", blocked.Damage, report.Damage)
	}
}

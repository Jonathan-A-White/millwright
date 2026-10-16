package vault_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestTheCloudBookIsReadBackAsWrittenAndCommittedAlone(t *testing.T) {
	t.Parallel()
	here, _ := twoHosts(t)
	book := vault.CloudBook{Vault: vault.New(here)}
	ctx := context.Background()

	empty, err := book.Read(ctx)
	if err != nil || empty.Month != "" || len(empty.Boxes) != 0 {
		t.Fatalf("expected a vault with no book to hold the zero state, got %+v, %v", empty, err)
	}

	up := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	state := application.CloudState{Month: "2026-10", Hours: 7, Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}}}
	if err := book.Write(ctx, state, "up cloud1"); err != nil {
		t.Fatalf("writing the book: %v", err)
	}
	got, err := book.Read(ctx)
	if err != nil || got.Hours != 7 || len(got.Boxes) != 1 || !got.Boxes[0].Up.Equal(up) || !got.HeldUntil.IsZero() {
		t.Errorf("expected the book back as written, got %+v, %v", got, err)
	}
	if log := run(t, here, "git", "log", "-1", "--name-only", "--format=%s"); !strings.Contains(log, "mw cloud: up cloud1") || !strings.Contains(log, vault.CloudBookPath) {
		t.Errorf("expected one commit of the book saying why, got %q", log)
	}
	if status := run(t, here, "git", "status", "--porcelain"); status != "" {
		t.Errorf("expected nothing left uncommitted, got %q", status)
	}
}

// A cloud check cut short — mw told to stop by a signal, its caller's timeout
// — ends its context while it keeps the book: the book is committed all the
// same, never left written and uncommitted for every dispatch to refuse on
// (mw-gq6.462).
func TestTheCloudBookIsCommittedEvenWhenItsRunIsCutShort(t *testing.T) {
	t.Parallel()
	here, _ := twoHosts(t)
	book := vault.CloudBook{Vault: vault.New(here)}
	if err := book.Write(context.Background(), application.CloudState{Month: "2026-10"}, "a new month"); err != nil {
		t.Fatalf("writing the book the first time: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	state := application.CloudState{Month: "2026-10", Hours: 3, Boxes: []application.CloudBox{{Name: "cloud1", Making: true}}}
	if err := book.Write(ctx, state, "making cloud1"); err != nil {
		t.Errorf("expected the book kept though the run was cut short, got %v", err)
	}
	if status := run(t, here, "git", "status", "--porcelain"); status != "" {
		t.Errorf("expected nothing left uncommitted for dispatch to refuse on, got %q", status)
	}
	if log := run(t, here, "git", "log", "-1", "--name-only", "--format=%s"); !strings.Contains(log, "mw cloud: making cloud1") || !strings.Contains(log, vault.CloudBookPath) {
		t.Errorf("expected the book committed saying why, got %q", log)
	}
}

// The fix the dirty-vault mail prints is run as a person would paste it: it
// commits the book left dirty, and nothing else the vault holds changed.
func TestTheDirtyVaultMailsFixCommitsTheBookAsFound(t *testing.T) {
	t.Parallel()
	here, _ := twoHosts(t)
	book := vault.CloudBook{Vault: vault.New(here)}
	if err := book.Write(context.Background(), application.CloudState{Month: "2026-10"}, "a new month"); err != nil {
		t.Fatalf("writing the book: %v", err)
	}
	write(t, here, vault.CloudBookPath, `{"month": "2026-10", "hours": 4}`+"\n")
	write(t, here, "seats/mayor/ledger.md", "a line nobody committed\n")
	files, err := vault.New(here).Uncommitted(context.Background())
	if err != nil || len(files) != 2 {
		t.Fatalf("expected the book and the ledger dirty, got %v, %v", files, err)
	}

	run(t, here, "sh", "-c", application.VaultBlockedFix(here, "laptop", []string{vault.CloudBookPath}))

	if status := run(t, here, "git", "status", "--porcelain"); status != " M seats/mayor/ledger.md\n" {
		t.Errorf("expected the book committed and the ledger left alone, got %q", status)
	}
	if got, err := book.Read(context.Background()); err != nil || got.Hours != 4 {
		t.Errorf("expected the book committed as found, got %+v, %v", got, err)
	}
}

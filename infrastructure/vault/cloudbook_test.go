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

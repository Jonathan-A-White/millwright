package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestSubscribeFilesAreListedAndRead(t *testing.T) {
	dir := t.TempDir()
	for seat, text := range map[string]string{"mayor": "kinds = [\"mail\"]\n", "deputy": "kinds = [\"message\"]\n"} {
		if err := os.MkdirAll(filepath.Join(dir, "seats", seat), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "seats", seat, "subscribe.toml"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "seats", "builder"), 0o755); err != nil {
		t.Fatal(err)
	}
	v := vault.New(dir)
	seats, err := v.SubscribedSeats(context.Background())
	if err != nil || strings.Join(seats, ",") != "deputy,mayor" {
		t.Fatalf("seats %v, %v; want deputy and mayor, in order, not builder", seats, err)
	}
	text, found, err := v.SubscribeFile(context.Background(), "mayor")
	if err != nil || !found || !strings.Contains(text, "mail") {
		t.Fatalf("got %q, %v, %v", text, found, err)
	}
	if _, found, err := v.SubscribeFile(context.Background(), "builder"); err != nil || found {
		t.Fatalf("a seat with no file: found=%v err=%v", found, err)
	}
	if _, _, err := v.SubscribeFile(context.Background(), "../x"); err == nil {
		t.Fatal("a seat name that reaches outside the vault was read")
	}
}

func TestSubscribedSeatsOfAVaultWithNoSeatsIsNone(t *testing.T) {
	seats, err := vault.New(t.TempDir()).SubscribedSeats(context.Background())
	if err != nil || len(seats) != 0 {
		t.Fatalf("got %v, %v", seats, err)
	}
}

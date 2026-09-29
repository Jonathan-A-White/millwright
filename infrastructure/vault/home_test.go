package vault_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestReadHomeReturnsTheFilesText(t *testing.T) {
	dir := aVault(t)
	want := "laptop 2026-09-29T00:10:00Z mw@laptop\n"
	if err := os.WriteFile(filepath.Join(dir, "home"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := vault.New(dir).ReadHome(context.Background())
	if err != nil || got != want {
		t.Errorf("got %q, %v, wanted %q", got, err, want)
	}
}

func TestReadHomeSaysSoWhenThereIsNoFile(t *testing.T) {
	_, err := vault.New(aVault(t)).ReadHome(context.Background())
	if !errors.Is(err, application.ErrNoHomeFile) {
		t.Errorf("expected ErrNoHomeFile, got %v", err)
	}
}

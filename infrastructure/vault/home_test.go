package vault_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
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

func TestWriteHomeReplacesTheFileWholeAndLeavesNothingBeside(t *testing.T) {
	dir := aVault(t)
	if err := os.WriteFile(filepath.Join(dir, "home"), []byte("desktop 2026-09-29T12:00:00Z mayor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := domain.HomeRecord{Host: "laptop", At: time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC), By: "mw@laptop"}

	if err := vault.New(dir).WriteHome(context.Background(), record); err != nil {
		t.Fatalf("WriteHome: %v", err)
	}

	got, err := vault.New(dir).ReadHome(context.Background())
	if err != nil || got != "laptop 2026-09-29T14:00:00Z mw@laptop\n" {
		t.Errorf("read back %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "home"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("the home file's mode: %v, %v", info, err)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, ".home-*")); len(entries) != 0 {
		t.Errorf("left a temporary file behind: %v", entries)
	}
}

func TestWriteHomeMakesTheFileWhenThereIsNone(t *testing.T) {
	dir := aVault(t)
	record := domain.HomeRecord{Host: "desktop", At: time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC), By: "mw@desktop"}
	if err := vault.New(dir).WriteHome(context.Background(), record); err != nil {
		t.Fatalf("WriteHome: %v", err)
	}
	if got, err := vault.New(dir).ReadHome(context.Background()); err != nil || got != record.String() {
		t.Errorf("read back %q, %v", got, err)
	}
}

package postern_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

func TestHandFileReadReportsAMissingFileAsNotExisting(t *testing.T) {
	h := postern.NewHandFile()
	text, exists, err := h.Read(context.Background(), filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if exists || text != "" {
		t.Fatalf("Read = %q, %v; want \"\", false", text, exists)
	}
}

func TestHandFileWriteThenReadRoundTrips(t *testing.T) {
	h := postern.NewHandFile()
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	ctx := context.Background()

	if err := h.Write(ctx, path, "vault = \"/x\"\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	text, exists, err := h.Read(ctx, path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !exists || text != "vault = \"/x\"\n" {
		t.Fatalf("Read = %q, %v; want the written text, true", text, exists)
	}
}

func TestHandFileBackupCopiesTheCurrentContentAndNeverTouchesTheOriginal(t *testing.T) {
	h := &postern.HandFile{Now: func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }}
	dir := t.TempDir()
	path := filepath.Join(dir, "site.conf")
	backupDir := filepath.Join(dir, "backups")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	backupPath, err := h.Backup(context.Background(), path, backupDir)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("reading the backup: %v", err)
	}
	if string(data) != "original\n" {
		t.Fatalf("the backup holds %q, want %q", data, "original\n")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "original\n" {
		t.Fatalf("the original was changed: %q", original)
	}
}

func TestHandFileMkdirAllMakesMissingParents(t *testing.T) {
	h := postern.NewHandFile()
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := h.MkdirAll(context.Background(), dir); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}

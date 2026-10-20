package grist_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// The spent mark is {"spent_at"}, 0600, and gone once cleared.
func TestTheSpentMarkIsKeptAndCleared(t *testing.T) {
	dir := t.TempDir()
	state := &grist.CreditState{Dir: dir}
	if _, found, err := state.Mark(context.Background()); err != nil || found {
		t.Fatalf("expected no mark, got %v %v", found, err)
	}
	at := time.Date(2026, time.October, 20, 9, 0, 0, 0, time.UTC)
	if err := state.Spend(context.Background(), application.GristCreditMark{SpentAt: at}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, grist.CreditsFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected %s kept 0600, got %v %v", grist.CreditsFile, info, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, grist.CreditsFile))
	if !strings.Contains(string(raw), `"spent_at":"2026-10-20T09:00:00Z"`) {
		t.Fatalf("expected spent_at in the file, got %s", raw)
	}
	mark, found, err := state.Mark(context.Background())
	if err != nil || !found || !mark.SpentAt.Equal(at) {
		t.Fatalf("expected the mark read back, got %v %v %v", mark, found, err)
	}
	if err := state.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := state.Mark(context.Background()); found {
		t.Fatal("expected the mark cleared")
	}
	if err := state.Clear(context.Background()); err != nil {
		t.Fatalf("expected clearing no mark to be fine, got %v", err)
	}
}

// An empty file, or {}, is no mark.
func TestAnEmptyCreditsFileIsNoMark(t *testing.T) {
	for _, said := range []string{"", "{}\n"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, grist.CreditsFile), []byte(said), 0o600)
		if _, found, err := (&grist.CreditState{Dir: dir}).Mark(context.Background()); err != nil || found {
			t.Errorf("%q: expected no mark, got %v %v", said, found, err)
		}
	}
}

// A key file is used only at mode 0600; anything else is refused with a note
// that names the file and not the key.
func TestTheKeyFileIsUsedOnlyAtMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic-api.key")
	read := func() (string, string) { return grist.CreditKeyFile{Path: path}.Read(context.Background()) }

	if key, why := read(); key != "" || !strings.Contains(why, path) {
		t.Fatalf("expected a missing file refused, naming it, got %q %q", key, why)
	}
	os.WriteFile(path, []byte("sk-ant-secret\n"), 0o644)
	os.Chmod(path, 0o644)
	if key, why := read(); key != "" || !strings.Contains(why, "0644") || strings.Contains(why, "sk-ant-secret") {
		t.Fatalf("expected mode 0644 refused without the key in the note, got %q %q", key, why)
	}
	os.Chmod(path, 0o600)
	if key, why := read(); key != "sk-ant-secret" || why != "" {
		t.Fatalf("expected the key at 0600, got %q %q", key, why)
	}
	os.WriteFile(path, []byte("\n"), 0o600)
	if key, why := read(); key != "" || why == "" {
		t.Fatalf("expected an empty file refused, got %q %q", key, why)
	}
}

func TestCreditKeyFileWriteIsAtomicOwnerOnlyAndReplacesAWideFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "anthropic-api.key")
	key := grist.CreditKeyFile{Path: path}
	if present, err := key.Present(context.Background()); err != nil || present {
		t.Fatalf("expected no file, got %v %v", present, err)
	}
	if err := key.Write(context.Background(), "sk-ant-one"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := key.Write(context.Background(), "sk-ant-two"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v %v", info, err)
	}
	if got, refused := key.Read(context.Background()); got != "sk-ant-two" || refused != "" {
		t.Errorf("expected the new key, got %q %q", got, refused)
	}
	if present, _ := key.Present(context.Background()); !present {
		t.Error("expected the file present")
	}
	left, _ := os.ReadDir(filepath.Dir(path))
	if len(left) != 1 {
		t.Errorf("expected no temporary file left, got %v", left)
	}
}

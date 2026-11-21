package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// handoffsDir makes seats/mayor/handoffs in a new vault with the named files.
func handoffsDir(t *testing.T, names ...string) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	handoffs := filepath.Join(dir, "seats", "mayor", "handoffs")
	if err := os.MkdirAll(handoffs, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(handoffs, name), []byte("text of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return vault.New(dir)
}

// The handoff counter passed 999: by text 2026-11-21-1000 sorts below
// 2026-11-21-999, and a Mayor booted from the older one.
func TestNewestVaultFileComparesRunsOfDigitsAsNumbers(t *testing.T) {
	names := []string{"2026-11-21-998.md", "2026-11-21-999.md", "2026-11-21-1000.md"}
	v := handoffsDir(t, names...)
	name, text, err := v.NewestVaultFile(context.Background(), "seats/mayor/handoffs")
	if err != nil {
		t.Fatal(err)
	}
	if want := "seats/mayor/handoffs/2026-11-21-1000.md"; name != want || text != "text of 2026-11-21-1000.md" {
		t.Errorf("the newest handoff is %q (%q), want %q", name, text, want)
	}

	v = handoffsDir(t, append(names, "2026-11-22-1.md")...)
	name, _, err = v.NewestVaultFile(context.Background(), "seats/mayor/handoffs")
	if err != nil {
		t.Fatal(err)
	}
	if want := "seats/mayor/handoffs/2026-11-22-1.md"; name != want {
		t.Errorf("with a later date the newest handoff is %q, want %q", name, want)
	}
}

// The list a seat's start reads has the newest handoff last, in the same order.
func TestSeatHandoffsAreOrderedByNumberNotText(t *testing.T) {
	v := handoffsDir(t, "2026-11-21-1000.md", "2026-11-21-999.md", "2026-11-21-98.md", "2026-11-22-1.md")
	start, err := v.SeatStart(context.Background(), "mayor", "laptop")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range start.Handoffs {
		got = append(got, h.Name)
	}
	want := []string{"2026-11-21-98", "2026-11-21-999", "2026-11-21-1000", "2026-11-22-1"}
	if len(got) != len(want) {
		t.Fatalf("handoffs %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("handoffs %v, want %v", got, want)
		}
	}
}

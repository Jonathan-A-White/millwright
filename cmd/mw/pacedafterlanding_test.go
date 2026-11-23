package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// The vault's paced lists are the release-order*.txt files and the *-order.txt
// files of a plan, each once (mw-gq6.1682): David's list is the second kind.
func TestVaultPacedListsFindsTheReleaseOrderAndTheNamedOrderListsOnce(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"plans/a/release-order.txt":       "# first\na-1\n\na-2\n",
		"plans/b/david-timings-order.txt": "b-1\n",
		"plans/b/notes.txt":               "not-a-list\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lists, err := vaultPacedLists{dir: dir}.PacedLists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 {
		t.Fatalf("expected 2 lists, got %+v", lists)
	}
	got := map[string]int{}
	for _, list := range lists {
		got[list.Name] = len(list.IDs)
	}
	if got["plans/a/release-order.txt"] != 2 || got["plans/b/david-timings-order.txt"] != 1 {
		t.Errorf("expected a's 2 ids and b's 1, got %v", got)
	}
}

// A landing host that says nothing of [pace] keeps the count the vault's pace.toml
// says, so the home's 5 is the count of a landing on the desktop or a cloud host
// too (mw-gq6.1698).
func TestPacedAfterLandingKeepsTheVaultsMaxAtOnceOnAHostThatSaysNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MW_VAULT", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pace.toml"), []byte("[pace]\nmax_at_once = 5\nusage_line = 70\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	released, ok := pacedAfterLanding(dir, nil).(application.PacedAfterLanding)
	if !ok {
		t.Fatalf("the paced release was not read: %v", pacedAfterLanding(dir, nil).ReleaseAfter(context.Background(), nil))
	}
	if released.Release.MaxAtOnce != 5 {
		t.Errorf("max_at_once: got %d, want the vault's 5", released.Release.MaxAtOnce)
	}
	if released.Release.UsageLine != 70 {
		t.Errorf("usage_line: got %v, want the vault's 70", released.Release.UsageLine)
	}
}

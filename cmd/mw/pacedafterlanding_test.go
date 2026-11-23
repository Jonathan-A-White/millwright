package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

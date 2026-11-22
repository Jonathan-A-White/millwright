package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// Two hosts landing in turn must not both call their batch 633: the second
// numbers above what the first put in the vault's origin, whatever its own
// checkout has pulled (mw-gq6.1662).
func TestNewBatchNumbersAboveEveryBatchTheOriginHolds(t *testing.T) {
	here, there := twoHosts(t)
	ctx := context.Background()

	// This host's checkout has batches up to 632.
	write(t, here, "runs/batch/lampas/632/outcome.txt", "landed aaa on main, 1 stories: x\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "batch 632")
	run(t, here, "git", "push", "-q")
	run(t, there, "git", "pull", "-q")

	// The other host landed 633 and pushed it; this checkout has not pulled.
	write(t, there, "runs/batch/lampas/633/outcome.txt", "landed bbb on main, 1 stories: y\n")
	run(t, there, "git", "add", "-A")
	run(t, there, "git", "commit", "-qm", "batch 633")
	run(t, there, "git", "push", "-q")

	got, err := vault.New(here).NewBatch(ctx, "lampas")
	if err != nil || got != 634 {
		t.Fatalf("expected batch 634 of lampas above the origin's 633, got %d (%v)", got, err)
	}
	// Another rig's folders are not this rig's.
	if got, err := vault.New(here).NewBatch(ctx, "millwright"); err != nil || got != 1 {
		t.Fatalf("expected batch 1 of millwright, got %d (%v)", got, err)
	}
	if note, err := os.ReadFile(filepath.Join(here, "runs/batch/lampas/634", vault.BatchNumberingNoteFile)); err == nil {
		t.Fatalf("expected no numbering note when the fetch worked, got %q", note)
	}
}

// A local folder above everything the origin holds still counts.
func TestNewBatchKeepsALocalFolderTheOriginLacks(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, "runs/batch/lampas/5/gate.log", "x")
	write(t, here, "runs/batch/lampas/4/gate.log", "x")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "batch 4 and 5")
	run(t, here, "git", "push", "-q")
	if err := os.Mkdir(filepath.Join(here, "runs/batch/lampas/9"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := vault.New(here).NewBatch(context.Background(), "lampas")
	if err != nil || got != 10 {
		t.Fatalf("expected batch 10 above the local 9, got %d (%v)", got, err)
	}
}

// A fetch that fails is said in the batch's notes, and the local folders number
// the batch as before.
func TestNewBatchSaysAFailedFetchAndNumbersFromTheLocalFolders(t *testing.T) {
	here, _ := twoHosts(t)
	write(t, here, "runs/batch/lampas/632/outcome.txt", "x")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "batch 632")
	run(t, here, "git", "push", "-q")
	// The origin goes away.
	run(t, here, "git", "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	got, err := vault.New(here).NewBatch(context.Background(), "lampas")
	if err != nil || got != 633 {
		t.Fatalf("expected batch 633 from the local folders, got %d (%v)", got, err)
	}
	note, err := os.ReadFile(filepath.Join(here, "runs/batch/lampas/633", vault.BatchNumberingNoteFile))
	if err != nil || !strings.Contains(string(note), "could not be fetched") {
		t.Fatalf("expected the failed fetch said in the batch's notes, got %q (%v)", note, err)
	}
}

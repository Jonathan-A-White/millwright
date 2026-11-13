package vault_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

var _ application.VaultDamageReader = (*vault.Vault)(nil)

// flipPack packs every object of the clone and flips the bytes of the pack
// between its header and its checksum, the way a bad disk or a cut-off write
// leaves one (mw-gq6.1167).
func flipPack(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "repack", "-adq")
	run(t, dir, "git", "prune-packed")
	packs, err := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "*.pack"))
	if err != nil || len(packs) == 0 {
		t.Fatalf("expected a pack to corrupt, got %v (%v)", packs, err)
	}
	for _, pack := range packs {
		body, err := os.ReadFile(pack)
		if err != nil {
			t.Fatal(err)
		}
		for i := 12; i < len(body)-20; i++ {
			body[i] ^= 0xff
		}
		if err := os.Chmod(pack, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pack, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDamageIsNoneForASoundVault(t *testing.T) {
	here, _ := twoHosts(t)
	got, err := vault.New(here).Damage(context.Background())
	if err != nil || got.Any() {
		t.Fatalf("expected a sound vault to show no damage, got %+v (%v)", got, err)
	}
	// Uncommitted work is not damage.
	write(t, here, "seats/mayor/ledger.md", "an edit")
	if got, err = vault.New(here).Damage(context.Background()); err != nil || got.Any() {
		t.Fatalf("expected an edited vault to show no damage, got %+v (%v)", got, err)
	}
}

func TestDamageNamesACorruptPack(t *testing.T) {
	here, _ := twoHosts(t)
	flipPack(t, here)

	got, err := vault.New(here).Damage(context.Background())
	if err != nil {
		t.Fatalf("expected the damage to be read, got %v", err)
	}
	if got.Corrupt == "" {
		t.Fatalf("expected a pack with its bytes flipped to be corrupt, got %+v", got)
	}
	if strings.Contains(got.Corrupt, "\n") {
		t.Errorf("expected one line of git's words, got %q", got.Corrupt)
	}
}

func TestDamageNamesARebaseThatStopped(t *testing.T) {
	here, there := twoHosts(t)
	write(t, there, "seats/mayor/ledger.md", "the other host's line\n")
	run(t, there, "git", "commit", "-qam", "The other host")
	run(t, there, "git", "push", "-q")
	write(t, here, "seats/mayor/ledger.md", "this host's line\n")
	run(t, here, "git", "commit", "-qam", "This host")
	run(t, here, "git", "fetch", "-q")
	rebase := gittest.Command("git", "rebase", "origin/main")
	rebase.Dir = here
	if err := rebase.Run(); err == nil {
		t.Fatal("expected the rebase to stop on a conflict")
	}

	got, err := vault.New(here).Damage(context.Background())
	if err != nil || !got.Rebasing || got.Corrupt != "" {
		t.Fatalf("expected a stopped rebase and no corruption, got %+v (%v)", got, err)
	}
	// The cheap look a sync takes every run finds it too (mw-gq6.1206).
	if stopped, err := vault.New(here).StoppedRebase(context.Background()); err != nil || !stopped {
		t.Fatalf("expected the stopped rebase to be seen, got %v (%v)", stopped, err)
	}
}

func TestStoppedRebaseIsFalseForASoundVault(t *testing.T) {
	here, _ := twoHosts(t)
	if stopped, err := vault.New(here).StoppedRebase(context.Background()); err != nil || stopped {
		t.Fatalf("expected no stopped rebase, got %v (%v)", stopped, err)
	}
}

// flipABlobInPack packs every object of the clone and flips the bytes of one
// blob's entry in the pack, leaving every commit and tree as it was, so that
// HEAD's tree still reads and the damage sits elsewhere (mw-gq6.1182).
func flipABlobInPack(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "repack", "-adq")
	run(t, dir, "git", "prune-packed")
	idxs, err := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "*.idx"))
	if err != nil || len(idxs) == 0 {
		t.Fatalf("expected a pack to corrupt, got %v (%v)", idxs, err)
	}
	for _, idx := range idxs {
		pack := strings.TrimSuffix(idx, ".idx") + ".pack"
		listed := gittest.Command("git", "verify-pack", "-v", idx)
		listed.Dir = dir
		said, err := listed.Output()
		if err != nil {
			t.Fatalf("expected the pack to list: %v", err)
		}
		var offset, size int
		found := false
		type entry struct{ offset, size int }
		var entries []entry
		for _, line := range strings.Split(string(said), "\n") {
			fields := strings.Fields(line)
			// <sha> <type> <size> <size-in-pack> <offset> ...
			if len(fields) >= 5 && fields[1] == "blob" {
				var e entry
				if _, err := fmt.Sscan(fields[3], &e.size); err != nil {
					continue
				}
				if _, err := fmt.Sscan(fields[4], &e.offset); err != nil {
					continue
				}
				entries = append(entries, e)
			}
		}
		if len(entries) > 0 {
			offset, size, found = entries[0].offset, entries[0].size, true
		}
		if !found {
			t.Fatal("expected a blob in the pack")
		}
		body, err := os.ReadFile(pack)
		if err != nil {
			t.Fatal(err)
		}
		// Past the entry's header, through its compressed data.
		for i := offset + 2; i < offset+size && i < len(body)-20; i++ {
			body[i] ^= 0xff
		}
		if err := os.Chmod(pack, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pack, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDamageNamesACorruptPackWhoseHeadTreeStillReads(t *testing.T) {
	here, _ := twoHosts(t)
	flipABlobInPack(t, here)

	tree := gittest.Command("git", "cat-file", "-p", "HEAD^{tree}")
	tree.Dir = here
	if err := tree.Run(); err != nil {
		t.Fatalf("expected HEAD's tree to stay readable, got %v", err)
	}

	got, err := vault.New(here).Damage(context.Background())
	if err != nil {
		t.Fatalf("expected the damage to be read, got %v", err)
	}
	if got.Corrupt == "" {
		t.Fatalf("expected a pack corrupt beyond HEAD's tree to be named, got %+v", got)
	}
	if strings.Contains(got.Corrupt, "\n") {
		t.Errorf("expected one line of git's words, got %q", got.Corrupt)
	}
}

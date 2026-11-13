package vault_test

import (
	"context"
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
}

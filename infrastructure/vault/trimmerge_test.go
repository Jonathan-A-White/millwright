package vault_test

// A rig memory is appended to by close-outs on every host and pruned by the
// Mayor. Merged by union, a prune synced while the other host appended a fact
// brought the pruned facts back; the vault's .gitattributes names the file
// with mw's trim-safe driver instead, which mw passes on every pull it makes,
// so no clone's git config names it (mw-gq6.499).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

const (
	memoryFile   = "seats/builder/rigs/millwright.md"
	memoryBase   = "# Builder's memory\n\n- fact one\n- fact two\n- fact three\n- fact four\n"
	memoryPruned = "# Builder's memory\n\n- fact one\n- fact two\n"
	memoryAdded  = "- fact five, from a close-out\n"
)

// aPruneAgainstAnAppend gives both clones a rig memory named merge=trimsafe,
// then prunes two facts in here, unpushed, while there appends one and pushes.
func aPruneAgainstAnAppend(t *testing.T) (here, there string) {
	t.Helper()
	here, there = twoHosts(t)
	write(t, here, ".gitattributes", "seats/*/ledger.md merge=union\nseats/*/rigs/*.md merge=trimsafe\n")
	write(t, here, memoryFile, memoryBase)
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "A rig memory")
	run(t, here, "git", "push", "-q")
	run(t, there, "git", "pull", "-q", "--rebase")

	write(t, here, memoryFile, memoryPruned)
	run(t, here, "git", "commit", "-qam", "The Mayor prunes two facts")
	write(t, there, memoryFile, memoryBase+memoryAdded)
	run(t, there, "git", "commit", "-qam", "A close-out appends a fact")
	run(t, there, "git", "push", "-q")
	return here, there
}

func TestASyncKeepsAPruneAndTheOtherHostsAppendedFact(t *testing.T) {
	here, there := aPruneAgainstAnAppend(t)
	ctx := context.Background()

	if _, err := vault.New(here).Pull(ctx); err != nil {
		t.Fatalf("pulling the appended fact onto the prune: %v", err)
	}
	if _, err := vault.New(here).Push(ctx); err != nil {
		t.Fatalf("pushing: %v", err)
	}
	if _, err := vault.New(there).Pull(ctx); err != nil {
		t.Fatalf("pulling the prune onto the other host: %v", err)
	}
	for _, dir := range []string{here, there} {
		got, err := os.ReadFile(filepath.Join(dir, memoryFile))
		if err != nil {
			t.Fatal(err)
		}
		if want := memoryPruned + memoryAdded; string(got) != want {
			t.Errorf("expected %s to hold the prune and the appended fact, got:\n%s", filepath.Base(dir), got)
		}
		for _, gone := range []string{"fact three", "fact four"} {
			if strings.Contains(string(got), gone) {
				t.Errorf("expected %q to stay pruned in %s, got:\n%s", gone, filepath.Base(dir), got)
			}
		}
		if said, err := exec.Command("git", "-C", dir, "config", "--get-regexp", `^merge\.`).CombinedOutput(); err == nil {
			t.Errorf("expected no merge driver in %s's git config, got %s", filepath.Base(dir), said)
		}
	}
}

// TestAPruneTheDriverCannotSettleStopsThePullAndNamesTheDriver: with a driver
// that fails, the pull is undone and says which driver could not settle which
// file, never union's both sides of the hunk.
func TestAPruneTheDriverCannotSettleStopsThePullAndNamesTheDriver(t *testing.T) {
	here, _ := aPruneAgainstAnAppend(t)
	head := run(t, here, "git", "rev-parse", "HEAD")

	_, err := vault.New(here, vault.WithMergeScript("#!/bin/sh\nexit 1\n")).Pull(context.Background())
	if err == nil {
		t.Fatal("expected the pull to stop")
	}
	if want := memoryFile + " merged by mw's trim-safe driver, merge=trimsafe"; !strings.Contains(err.Error(), want) {
		t.Errorf("expected the refusal to say %q, got %v", want, err)
	}
	if now := run(t, here, "git", "rev-parse", "HEAD"); now != head {
		t.Errorf("expected the vault where it was, HEAD went from %s to %s", head, now)
	}
	if left := run(t, here, "git", "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Errorf("expected no half-finished rebase left behind, got %q", left)
	}
	if got, _ := os.ReadFile(filepath.Join(here, memoryFile)); string(got) != memoryPruned {
		t.Errorf("expected this host's prune as it was, got:\n%s", got)
	}
}

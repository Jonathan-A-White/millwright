package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// corruptEveryPack overwrites the middle of each pack in the checkout, as the
// bad disk on cloud1 did, so that git cannot read the objects in it.
func corruptEveryPack(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "repack", "-adq")
	packs, _ := filepath.Glob(filepath.Join(dir, ".git", "objects", "pack", "*.pack"))
	if len(packs) == 0 {
		t.Fatal("expected a pack to corrupt")
	}
	for _, pack := range packs {
		data, err := os.ReadFile(pack)
		if err != nil {
			t.Fatal(err)
		}
		for i := 12; i < len(data)-20; i++ {
			data[i] = 0xff
		}
		if err := os.Chmod(pack, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pack, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestACheckoutWithACorruptPackFailsItsFetchAndIsClonedAgain(t *testing.T) {
	here, other := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()
	write(t, other, "newer.md", "a newer commit\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "A newer commit")
	run(t, other, "git", "push", "-q", "origin", "main")
	corruptEveryPack(t, here)

	err := worktrees.Fetch(ctx, here)
	if err == nil {
		t.Fatal("expected the fetch in a checkout with a corrupt pack to fail")
	}
	t.Logf("the fetch said: %v", err)

	if n, err := worktrees.Unpushed(ctx, here); err != nil || n != 0 {
		t.Errorf("expected a checkout level with its remote ref to hold nothing unpushed, got %d, %v", n, err)
	}
	aside := here + ".corrupt-2026-11-12"
	moved, err := worktrees.Reclone(ctx, here, aside, "main")
	if err != nil || moved != aside {
		t.Fatalf("expected the checkout moved to %s, got %q, %v", aside, moved, err)
	}
	if _, err := os.Stat(filepath.Join(aside, ".git")); err != nil {
		t.Errorf("expected the old checkout kept at %s: %v", aside, err)
	}
	if _, err := os.Stat(filepath.Join(here, "newer.md")); err != nil {
		t.Errorf("expected the new clone to hold origin's newest commit: %v", err)
	}
	if err := worktrees.Fetch(ctx, here); err != nil {
		t.Errorf("expected the fetch to work in the new clone: %v", err)
	}
	var _ application.CheckoutHealer = worktrees
}

func TestUnpushedCountsCommitsNoRemoteRefHolds(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()
	write(t, here, "mine.md", "mine\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Mine")

	if n, err := worktrees.Unpushed(ctx, here); err != nil || n != 1 {
		t.Errorf("expected one unpushed commit, got %d, %v", n, err)
	}
}

func TestRecloneTakesAnotherNameWhenTheAsideIsTakenAndPutsTheCheckoutBackWhenTheCloneFails(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()
	aside := here + ".corrupt-2026-11-12"
	if err := os.Mkdir(aside, 0o755); err != nil {
		t.Fatal(err)
	}
	moved, err := worktrees.Reclone(ctx, here, aside, "main")
	if err != nil || moved != aside+"-2" {
		t.Fatalf("expected the second name, got %q, %v", moved, err)
	}

	// A branch origin lacks cannot be cloned: the checkout is put back.
	moved, err = worktrees.Reclone(ctx, here, here+".corrupt-b", "no-such-branch")
	if err == nil || !strings.Contains(err.Error(), "no-such-branch") {
		t.Fatalf("expected the clone to fail naming the branch, got %q, %v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(here, ".git")); err != nil {
		t.Errorf("expected the checkout put back: %v", err)
	}
}

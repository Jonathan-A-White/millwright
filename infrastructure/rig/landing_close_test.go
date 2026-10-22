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

// landingGone fails the test when the landing directory is still there.
func landingGone(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected the landing directory %s to be gone, stat said %v", dir, err)
	}
}

// TestAVerifiedLandingLeavesNoDirectoryBehind drives real git through the
// whole landing — merge, push, close — and looks for the throwaway directory
// beside the rig afterwards (mw-gq6.698).
func TestAVerifiedLandingLeavesNoDirectoryBehind(t *testing.T) {
	here, _ := aRig(t)
	landed(t, here)

	left, err := filepath.Glob(filepath.Join(filepath.Dir(here), application.LandingPrefix+"*"))
	if err != nil {
		t.Fatalf("looking for landing directories: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("expected no landing directory left beside the rig, found %v", left)
	}
}

// TestCloseLandingTakesTheDirectoryAwayWhenGitHasLostTrackOfIt covers the
// orphan the Laptop collected: git's own record of the worktree is gone, so
// `git worktree remove` refuses, and the directory stayed.
func TestCloseLandingTakesTheDirectoryAwayWhenGitHasLostTrackOfIt(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(dir), application.LandingPrefix) {
		t.Fatalf("expected the landing to be named after %q, got %s", application.LandingPrefix, dir)
	}
	admin, err := filepath.Glob(filepath.Join(here, ".git", "worktrees", "*"+filepath.Base(dir)[1:]))
	if err != nil || len(admin) != 1 {
		t.Fatalf("expected git's one record of the landing, got %v (%v)", admin, err)
	}
	if err := os.RemoveAll(admin[0]); err != nil {
		t.Fatalf("losing git's record of the landing: %v", err)
	}

	if err := worktrees.CloseLanding(ctx, here, dir); err != nil {
		t.Fatalf("closing a landing git has lost track of: %v", err)
	}
	landingGone(t, dir)
}

// TestCloseLandingStillWorksWhenTheCloseOutWasCancelled covers a close-out
// stopped mid-landing: its context is done, and the landing is taken away
// all the same.
func TestCloseLandingStillWorksWhenTheCloseOutWasCancelled(t *testing.T) {
	here, _ := aRig(t)
	worktrees := rig.New()

	ctx, cancel := context.WithCancel(context.Background())
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	cancel()

	if err := worktrees.CloseLanding(ctx, here, dir); err != nil {
		t.Fatalf("closing the landing of a cancelled close-out: %v", err)
	}
	landingGone(t, dir)
}

// TestCloseLandingNeverRemovesADirectoryThatIsNotALanding keeps the fallback
// to the landing prefix.
func TestCloseLandingNeverRemovesADirectoryThatIsNotALanding(t *testing.T) {
	here, _ := aRig(t)
	other := filepath.Join(t.TempDir(), "keep-me")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = rig.New().CloseLanding(context.Background(), here, other)
	if _, err := os.Stat(other); err != nil {
		t.Errorf("expected a directory that is not a landing to be left alone: %v", err)
	}
}

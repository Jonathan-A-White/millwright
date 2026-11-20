package rig_test

// A landing pushes every commit of its branch, so the check for a private term
// reads each commit's added lines, not only the tip (mw-gq6.1543). The terms
// here are invented; no real term is in this file.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

const inventedTerm = "zorblax"

// aRigWithTheCheck is aRig whose repository tracks scripts/check-private-terms.sh
// (as millwright's does), with an invented list and an empty baseline named by
// MW_PRIVATE_TERMS for the test.
func aRigWithTheCheck(t *testing.T) (here string) {
	t.Helper()
	here, _ = aRig(t)
	script, err := os.ReadFile("../../scripts/check-private-terms.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(here, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(here, "scripts/check-private-terms.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The check")
	run(t, here, "git", "push", "-q", "origin", "main")

	dir := t.TempDir()
	list := filepath.Join(dir, "private-terms.txt")
	if err := os.WriteFile(list, []byte("# invented\n"+inventedTerm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private-terms.baseline"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MW_PRIVATE_TERMS", list)
	return here
}

// aLandingOf opens a landing on origin/main, merges the branch and tries the push.
func aLandingOf(t *testing.T, here, branch string) error {
	t.Helper()
	ctx := context.Background()
	worktrees := rig.New()
	run(t, here, "git", "fetch", "-q", "origin")
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	defer worktrees.CloseLanding(ctx, here, dir)
	if _, err := worktrees.Merge(ctx, dir, branch); err != nil {
		t.Fatalf("merging: %v", err)
	}
	return worktrees.Push(ctx, dir, "origin", "main")
}

func TestPrivateTermsALandingRefusesToPushACommitHoldingAPrivateTermAGoneByTheTip(t *testing.T) {
	here := aRigWithTheCheck(t)
	before := run(t, here, "git", "rev-parse", "origin/main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/story", "main")
	write(t, here, "notes.md", "the "+inventedTerm+" fixture\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Add the fixture")
	first := run(t, here, "git", "rev-parse", "HEAD")
	write(t, here, "notes.md", "a clean fixture\n")
	run(t, here, "git", "commit", "-qam", "Keep the fixture clean")
	run(t, here, "git", "checkout", "-q", "main")

	err := aLandingOf(t, here, "mw/story")
	if err == nil || !errors.Is(err, application.ErrPushPrivateTerm) {
		t.Fatalf("expected the push refused for a private term, got %v", err)
	}
	if !strings.Contains(err.Error(), first[:12]) || !strings.Contains(err.Error(), "notes.md") {
		t.Errorf("expected the commit and file named, got %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), inventedTerm) || strings.Contains(err.Error(), "fixture") {
		t.Errorf("the refusal holds the term or a line's text: %v", err)
	}
	run(t, here, "git", "fetch", "-q", "origin")
	if after := run(t, here, "git", "rev-parse", "origin/main"); after != before {
		t.Errorf("expected nothing pushed, origin/main moved %s to %s", before, after)
	}
}

func TestPrivateTermsALandingPushesACleanRangeWithTheCheckInPlace(t *testing.T) {
	here := aRigWithTheCheck(t)
	aBranchAppending(t, here, "mw/story", "README.md", "a clean line")
	if err := aLandingOf(t, here, "mw/story"); err != nil {
		t.Fatalf("expected the push, got %v", err)
	}
	run(t, here, "git", "fetch", "-q", "origin")
	if got := run(t, here, "git", "show", "origin/main:README.md"); !strings.Contains(got, "a clean line") {
		t.Errorf("expected the landing on origin/main, got %q", got)
	}
}

func TestPrivateTermsALandingPushesWhereThereIsNoListOfPrivateTerms(t *testing.T) {
	here := aRigWithTheCheck(t)
	t.Setenv("MW_PRIVATE_TERMS", filepath.Join(t.TempDir(), "none.txt"))
	run(t, here, "git", "checkout", "-q", "-b", "mw/story", "main")
	write(t, here, "notes.md", "the "+inventedTerm+" fixture\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Add the fixture")
	run(t, here, "git", "checkout", "-q", "main")
	if err := aLandingOf(t, here, "mw/story"); err != nil {
		t.Fatalf("expected the check skipped with no list, got %v", err)
	}
}

func TestPrivateTermsALandingOfARigWithoutTheCheckPushesWhatItHolds(t *testing.T) {
	here, _ := aRig(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "private-terms.txt"), []byte(inventedTerm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private-terms.baseline"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MW_PRIVATE_TERMS", filepath.Join(dir, "private-terms.txt"))
	aBranchAppending(t, here, "mw/story", "README.md", "the "+inventedTerm+" line")
	if err := aLandingOf(t, here, "mw/story"); err != nil {
		t.Fatalf("expected a rig that tracks no check to push, got %v", err)
	}
}

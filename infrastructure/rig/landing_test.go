package rig_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// TestCommitsReadsTheWholeMessageOfEveryCommitAboutToLand drives real git
// against a throwaway repository: what a close-out is about to put on the
// target branch, read back message and all, oldest first.
func TestCommitsReadsTheWholeMessageOfEveryCommitAboutToLand(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "first.md", "the first\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The first commit\n\nA body with a blank line in it.\n\nAnd a second paragraph.")
	write(t, here, "second.md", "the second\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The second commit\n\nCo-Authored-By: Somebody <somebody@example.invalid>")

	commits, err := worktrees.Commits(ctx, here, "mw/story", "main")
	if err != nil {
		t.Fatalf("reading the commits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected the two commits the branch is ahead by, got %d: %+v", len(commits), commits)
	}

	// Oldest first: the order they would land in.
	if !strings.HasPrefix(commits[0].Message, "The first commit") {
		t.Errorf("expected the oldest commit first, got %q", commits[0].Message)
	}
	for _, want := range []string{"A body with a blank line in it.", "And a second paragraph."} {
		if !strings.Contains(commits[0].Message, want) {
			t.Errorf("expected the whole message, %q is missing from %q", want, commits[0].Message)
		}
	}
	if line := application.AIAttribution(commits[1].Message); line != "Co-Authored-By: Somebody <somebody@example.invalid>" {
		t.Errorf("expected the trailer of the second commit to be read back whole, got %q", line)
	}

	for _, commit := range commits {
		if commit.Hash == "" || strings.ContainsAny(commit.Hash, " \n") {
			t.Errorf("expected a short hash on its own, got %q", commit.Hash)
		}
	}

	// A branch level with the target lands nothing, and reads back as nothing.
	none, err := worktrees.Commits(ctx, here, "main", "main")
	if err != nil {
		t.Fatalf("reading the commits of a branch level with the target: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected no commits, got %+v", none)
	}
}

func TestCommitsRefusesAHalfQuestion(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	if _, err := worktrees.Commits(ctx, here, "", "main"); err == nil {
		t.Error("expected reading the commits of no branch to be refused")
	}
	if _, err := worktrees.Commits(ctx, here, "main", ""); err == nil {
		t.Error("expected reading the commits ahead of nothing to be refused")
	}
}

// TestUncommittedNamesEveryChangedFileAndNothingElse drives real git: a
// modified tracked file, a deleted one, a staged new one and an untracked one in
// a directory are each named as a file, a rename is named once, and an ignored
// file and a clean worktree are named not at all.
func TestUncommittedNamesEveryChangedFileAndNothingElse(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	clean, err := worktrees.Uncommitted(ctx, here)
	if err != nil {
		t.Fatalf("reading a clean worktree: %v", err)
	}
	if len(clean) != 0 {
		t.Fatalf("expected a clean worktree to list nothing, got %q", clean)
	}

	write(t, here, "moved-from.md", "to be renamed\n")
	write(t, here, "gone.md", "to be deleted\n")
	write(t, here, ".gitignore", "*.log\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Files to disturb")

	write(t, here, "README.md", "changed in place\n")
	run(t, here, "git", "rm", "-q", "gone.md")
	run(t, here, "git", "mv", "moved-from.md", "moved-to.md")
	write(t, here, "staged.md", "new and staged\n")
	run(t, here, "git", "add", "staged.md")
	write(t, here, "notes/half-done.md", "new and untracked, in a directory\n")
	write(t, here, "build.log", "ignored\n")

	got, err := worktrees.Uncommitted(ctx, here)
	if err != nil {
		t.Fatalf("reading a dirty worktree: %v", err)
	}
	want := map[string]bool{
		"README.md": true, "gone.md": true, "moved-to.md": true, "staged.md": true, "notes/half-done.md": true,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d paths, got %d: %q", len(want), len(got), got)
	}
	for _, path := range got {
		if !want[path] {
			t.Errorf("did not expect %q among the uncommitted paths %q", path, got)
		}
	}
}

func TestUncommittedRefusesAHalfQuestion(t *testing.T) {
	if _, err := rig.New().Uncommitted(context.Background(), ""); err == nil {
		t.Error("expected an error when no worktree is named")
	}
}

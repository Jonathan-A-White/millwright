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

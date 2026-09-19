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

// landed lands a story's one commit on main at the origin the way mw next does —
// in a throwaway worktree, pushed from there — and leaves the rig checkout
// where it was, which is what Advance is for.
func landed(t *testing.T, here string) application.Landed {
	t.Helper()
	ctx := context.Background()
	worktrees := rig.New()

	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "story.md", "the story's work\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The story's work")
	run(t, here, "git", "checkout", "-q", "main")

	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	merged, err := worktrees.Merge(ctx, dir, "mw/story")
	if err != nil {
		t.Fatalf("merging the story: %v", err)
	}
	if err := worktrees.Push(ctx, dir, "origin", "main"); err != nil {
		t.Fatalf("pushing the landing: %v", err)
	}
	if err := worktrees.CloseLanding(ctx, here, dir); err != nil {
		t.Fatalf("closing the landing: %v", err)
	}
	return merged
}

// TestAdvanceBringsTheRigCheckoutToTheLandedCommit drives real git: a landing is
// made and pushed from a throwaway worktree, so the rig's own checkout is behind
// the main it just pushed until Advance fast-forwards it.
func TestAdvanceBringsTheRigCheckoutToTheLandedCommit(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	merged := landed(t, here)
	if got := run(t, here, "git", "rev-parse", "HEAD"); got == merged.Commit {
		t.Fatalf("expected the rig checkout to be behind the landing before Advance, but it is at %s", got)
	}

	advanced, err := worktrees.Advance(ctx, here, "main", merged.Commit)
	if err != nil {
		t.Fatalf("advancing the rig checkout: %v", err)
	}
	if !advanced.Moved || advanced.Left != "" {
		t.Errorf("expected the checkout to be moved with nothing left, got %+v", advanced)
	}
	if got := run(t, here, "git", "rev-parse", "HEAD"); got != merged.Commit {
		t.Errorf("expected the rig checkout at the landed commit %s, got %s", merged.Commit, got)
	}
	if got := run(t, here, "git", "rev-parse", "main"); got != merged.Commit {
		t.Errorf("expected main at the landed commit %s, got %s", merged.Commit, got)
	}
	if _, err := os.Stat(filepath.Join(here, "story.md")); err != nil {
		t.Errorf("expected the story's work in the rig checkout: %v", err)
	}

	// Once level there is nothing to move and nothing to say.
	again, err := worktrees.Advance(ctx, here, "main", merged.Commit)
	if err != nil {
		t.Fatalf("advancing a checkout already level: %v", err)
	}
	if again.Moved || again.Left != "" {
		t.Errorf("expected a level checkout to be neither moved nor left, got %+v", again)
	}
}

// TestAdvanceLeavesADirtyCheckoutAloneAndSaysSo: a person's work in progress in
// the rig checkout is never touched, tracked changes and stray files alike.
func TestAdvanceLeavesADirtyCheckoutAloneAndSaysSo(t *testing.T) {
	cases := []struct {
		name  string
		touch func(t *testing.T, here string) (path, content string)
	}{
		{"a modified tracked file", func(t *testing.T, here string) (string, string) {
			write(t, here, "README.md", "half-done edit\n")
			return "README.md", "half-done edit\n"
		}},
		{"an untracked file", func(t *testing.T, here string) (string, string) {
			write(t, here, "scratch.md", "a note to self\n")
			return "scratch.md", "a note to self\n"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			here, _ := aRig(t)
			merged := landed(t, here)
			before := run(t, here, "git", "rev-parse", "HEAD")
			path, content := tc.touch(t, here)

			advanced, err := rig.New().Advance(context.Background(), here, "main", merged.Commit)
			if err != nil {
				t.Fatalf("advancing a dirty checkout is left alone, not an error: %v", err)
			}
			if advanced.Moved {
				t.Errorf("expected a dirty checkout not to be moved, got %+v", advanced)
			}
			if !strings.Contains(advanced.Left, "uncommitted") || !strings.Contains(advanced.Left, path) {
				t.Errorf("expected the reason to say the checkout has uncommitted work and name %s, got %q", path, advanced.Left)
			}
			if got := run(t, here, "git", "rev-parse", "HEAD"); got != before {
				t.Errorf("expected HEAD left at %s, got %s", before, got)
			}
			if got, err := os.ReadFile(filepath.Join(here, path)); err != nil || string(got) != content {
				t.Errorf("expected %s left as it was, got %q (%v)", path, got, err)
			}
		})
	}
}

// TestAdvanceLeavesACheckoutOnAnotherBranchAlone: somebody working on a branch
// of their own in the rig checkout is not moved onto main, and neither is main.
func TestAdvanceLeavesACheckoutOnAnotherBranchAlone(t *testing.T) {
	here, _ := aRig(t)
	merged := landed(t, here)
	run(t, here, "git", "checkout", "-q", "-b", "wip")
	before := run(t, here, "git", "rev-parse", "HEAD")
	mainBefore := run(t, here, "git", "rev-parse", "main")

	advanced, err := rig.New().Advance(context.Background(), here, "main", merged.Commit)
	if err != nil {
		t.Fatalf("advancing a checkout on another branch is left alone, not an error: %v", err)
	}
	if advanced.Moved {
		t.Errorf("expected a checkout on another branch not to be moved, got %+v", advanced)
	}
	if !strings.Contains(advanced.Left, "wip") || !strings.Contains(advanced.Left, "main") {
		t.Errorf("expected the reason to name both wip and main, got %q", advanced.Left)
	}
	if got := run(t, here, "git", "rev-parse", "HEAD"); got != before {
		t.Errorf("expected HEAD left at %s, got %s", before, got)
	}
	if got := run(t, here, "git", "rev-parse", "main"); got != mainBefore {
		t.Errorf("expected main left at %s, got %s", mainBefore, got)
	}
}

// TestAdvanceLeavesACheckoutWithCommitsOfItsOwnAlone: a local main that has
// commits the remote does not is not fast-forwardable, and mw merges nothing
// into it.
func TestAdvanceLeavesACheckoutWithCommitsOfItsOwnAlone(t *testing.T) {
	here, _ := aRig(t)
	merged := landed(t, here)
	write(t, here, "local.md", "committed here, never pushed\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "A local commit")
	before := run(t, here, "git", "rev-parse", "HEAD")

	advanced, err := rig.New().Advance(context.Background(), here, "main", merged.Commit)
	if err != nil {
		t.Fatalf("advancing a diverged checkout is left alone, not an error: %v", err)
	}
	if advanced.Moved || advanced.Left == "" {
		t.Errorf("expected a diverged checkout to be left with a reason, got %+v", advanced)
	}
	if got := run(t, here, "git", "rev-parse", "HEAD"); got != before {
		t.Errorf("expected HEAD left at %s, got %s", before, got)
	}
}

func TestAdvanceRefusesAHalfQuestion(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	if _, err := rig.New().Advance(ctx, here, "", "abc"); err == nil {
		t.Error("expected advancing no branch to be refused")
	}
	if _, err := rig.New().Advance(ctx, here, "main", ""); err == nil {
		t.Error("expected advancing to no commit to be refused")
	}
}

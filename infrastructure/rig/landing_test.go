package rig_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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

// TestMergedIntoSeesABranchTheCloseOutMerged drives real git through the four
// shapes a story's branch is found in after its session died (mw-gq6.161).
func TestMergedIntoSeesABranchTheCloseOutMerged(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	// Cut through the worktree port, as a dispatch cuts it, and never committed to.
	cut := filepath.Join(filepath.Dir(here), "cut")
	if err := worktrees.Add(ctx, here, cut, "mw/cut", "origin/main"); err != nil {
		t.Fatalf("cutting the worktree: %v", err)
	}
	if tip, merged, err := worktrees.MergedInto(ctx, here, "mw/cut", "origin/main"); err != nil || merged || tip == "" {
		t.Fatalf("expected a branch that holds no work of its own not merged, got %q %v %v", tip, merged, err)
	}

	if tip, merged, err := worktrees.MergedInto(ctx, here, "mw/absent", "origin/main"); err != nil || merged || tip != "" {
		t.Fatalf("expected an absent branch not merged and no error, got %q %v %v", tip, merged, err)
	}

	// Work committed but not landed.
	work := filepath.Join(filepath.Dir(here), "work")
	if err := worktrees.Add(ctx, here, work, "mw/story", "origin/main"); err != nil {
		t.Fatalf("cutting the worktree: %v", err)
	}
	write(t, work, "story.md", "the story's work\n")
	run(t, work, "git", "add", "-A")
	run(t, work, "git", "commit", "-qm", "The story's work")
	if _, merged, err := worktrees.MergedInto(ctx, here, "mw/story", "origin/main"); err != nil || merged {
		t.Fatalf("expected a branch with commits main lacks not merged, got %v %v", merged, err)
	}

	// Landed, as a close-out does it.
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	if _, err := worktrees.Merge(ctx, dir, "mw/story"); err != nil {
		t.Fatalf("merging the story: %v", err)
	}
	if err := worktrees.Push(ctx, dir, "origin", "main"); err != nil {
		t.Fatalf("pushing the landing: %v", err)
	}
	if err := worktrees.CloseLanding(ctx, here, dir); err != nil {
		t.Fatalf("closing the landing: %v", err)
	}
	want := strings.TrimSpace(run(t, here, "git", "rev-parse", "mw/story"))
	if tip, merged, err := worktrees.MergedInto(ctx, here, "mw/story", "origin/main"); err != nil || !merged || tip != want {
		t.Fatalf("expected the landed branch merged at %s, got %q %v %v", want, tip, merged, err)
	}

	if _, _, err := worktrees.MergedInto(ctx, here, "", "origin/main"); err == nil {
		t.Fatalf("expected a half question refused")
	}
}

// TestAdvanceNamesAtMostTenOfAManyUncommittedPaths: a rig checkout with a
// hundred and sixty-five stray files gets a count, the first ten paths and the
// number left over, not a mail with every one of them in it.
func TestAdvanceNamesAtMostTenOfAManyUncommittedPaths(t *testing.T) {
	cases := []struct {
		name     string
		dirs     func(i int) string
		count    int
		want     []string
		wantNot  []string
		wantMore string
	}{
		{"a hundred and sixty-five under one directory", func(i int) string { return "coverage-stage3a/" }, 165,
			[]string{"it has 165 uncommitted path(s)", "all under coverage-stage3a/", "coverage-stage3a/f000.txt", "coverage-stage3a/f009.txt", "and 155 more"},
			[]string{"coverage-stage3a/f010.txt", "coverage-stage3a/f164.txt"}, "and 155 more"},
		{"a hundred and sixty-five in two directories", func(i int) string { return []string{"a/", "b/"}[i%2] }, 165,
			[]string{"it has 165 uncommitted path(s): ", "and 155 more"},
			[]string{"all under", "b/f001.txt"}, "and 155 more"},
		{"three under one directory", func(i int) string { return "scratch/" }, 3,
			[]string{"it has 3 uncommitted path(s), all under scratch/: ", "scratch/f000.txt", "scratch/f001.txt", "scratch/f002.txt"},
			[]string{"more"}, ""},
		{"three at the top", func(i int) string { return "" }, 3,
			[]string{"it has 3 uncommitted path(s): f000.txt, f001.txt, f002.txt"},
			[]string{"more", "all under"}, ""},
		{"exactly ten", func(i int) string { return "" }, 10,
			[]string{"it has 10 uncommitted path(s): ", "f009.txt"},
			[]string{"more"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			here, _ := aRig(t)
			merged := landed(t, here)
			for i := 0; i < tc.count; i++ {
				write(t, here, fmt.Sprintf("%sf%03d.txt", tc.dirs(i), i), "stray\n")
			}

			advanced, err := rig.New().Advance(context.Background(), here, "main", merged.Commit)
			if err != nil {
				t.Fatalf("advancing a dirty checkout is left alone, not an error: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(advanced.Left, want) {
					t.Errorf("expected the reason to contain %q, got %q", want, advanced.Left)
				}
			}
			for _, not := range tc.wantNot {
				if strings.Contains(advanced.Left, not) {
					t.Errorf("expected the reason not to contain %q, got %q", not, advanced.Left)
				}
			}
		})
	}
}

// landBranch lands branch on main at the origin the way mw next does, in a
// throwaway landing worktree, and reports the merge error if there was one.
func landBranch(t *testing.T, here, branch string) error {
	t.Helper()
	ctx := context.Background()
	worktrees := rig.New()
	run(t, here, "git", "fetch", "-q", "origin")
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	defer func() {
		if err := worktrees.CloseLanding(ctx, here, dir); err != nil {
			t.Errorf("closing the landing: %v", err)
		}
	}()
	if _, err := worktrees.Merge(ctx, dir, branch); err != nil {
		return err
	}
	return worktrees.Push(ctx, dir, "origin", "main")
}

// aClaudeMdBranch cuts a branch from main that appends line to the rig's CLAUDE.md.
func aClaudeMdBranch(t *testing.T, here, branch, line string) {
	t.Helper()
	run(t, here, "git", "checkout", "-q", "-b", branch, "main")
	f, err := os.OpenFile(filepath.Join(here, "CLAUDE.md"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening CLAUDE.md: %v", err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("appending to CLAUDE.md: %v", err)
	}
	f.Close()
	run(t, here, "git", "commit", "-qam", "Note: "+line)
	run(t, here, "git", "checkout", "-q", "main")
}

// TestTwoBranchesAppendingToClaudeMdLandOneAfterTheOtherWithNoConflict drives
// real git: both branches add a different last line to CLAUDE.md, which on its
// own conflicts, and a landing treats the file as a union merge so both stay.
func TestTwoBranchesAppendingToClaudeMdLandOneAfterTheOtherWithNoConflict(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "CLAUDE.md", "# notes\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "CLAUDE.md opens")
	run(t, here, "git", "push", "-q", "origin", "main")

	aClaudeMdBranch(t, here, "mw/one", "one learned this")
	aClaudeMdBranch(t, here, "mw/two", "two learned that")

	if err := landBranch(t, here, "mw/one"); err != nil {
		t.Fatalf("landing the first branch: %v", err)
	}
	if err := landBranch(t, here, "mw/two"); err != nil {
		t.Fatalf("landing the second branch, which only appended a line to CLAUDE.md: %v", err)
	}
	run(t, here, "git", "fetch", "-q", "origin")
	got := run(t, here, "git", "show", "origin/main:CLAUDE.md")
	for _, want := range []string{"# notes", "one learned this", "two learned that"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected CLAUDE.md on main to keep %q, got:\n%s", want, got)
		}
	}
	if tracked := run(t, here, "git", "ls-files", ".gitattributes"); tracked != "" {
		t.Errorf("expected no tracked .gitattributes to be made, got %q", tracked)
	}
}

// TestAConflictInAnotherFileStillStopsTheLanding keeps the union rule to
// CLAUDE.md: two different edits of the same line of any other file conflict.
func TestAConflictInAnotherFileStillStopsTheLanding(t *testing.T) {
	here, _ := aRig(t)
	for _, b := range []struct{ branch, text string }{{"mw/one", "one's README\n"}, {"mw/two", "two's README\n"}} {
		run(t, here, "git", "checkout", "-q", "-b", b.branch, "main")
		write(t, here, "README.md", b.text)
		run(t, here, "git", "commit", "-qam", "README "+b.branch)
		run(t, here, "git", "checkout", "-q", "main")
	}
	if err := landBranch(t, here, "mw/one"); err != nil {
		t.Fatalf("landing the first branch: %v", err)
	}
	if err := landBranch(t, here, "mw/two"); !application.Conflicted(err) {
		t.Errorf("expected the second README edit to conflict, got %v", err)
	}
}

// TestUnionNotesMakesARebaseKeepBothAppendedLines drives real git in a story
// worktree: after UnionNotes, a rebase onto a main that also appended to CLAUDE.md
// goes through with no stop.
func TestUnionNotesMakesARebaseKeepBothAppendedLines(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()
	write(t, here, "CLAUDE.md", "# notes\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "CLAUDE.md opens")
	run(t, here, "git", "push", "-q", "origin", "main")

	aClaudeMdBranch(t, here, "mw/one", "one learned this")
	aClaudeMdBranch(t, here, "mw/two", "two learned that")
	if err := landBranch(t, here, "mw/one"); err != nil {
		t.Fatalf("landing the first branch: %v", err)
	}

	work := filepath.Join(filepath.Dir(here), "work")
	if err := worktrees.Add(ctx, here, work, "mw/work", "mw/two"); err != nil {
		t.Fatalf("cutting the story worktree: %v", err)
	}
	if err := worktrees.UnionNotes(ctx, work); err != nil {
		t.Fatalf("setting up the union: %v", err)
	}
	if err := worktrees.UnionNotes(ctx, work); err != nil {
		t.Fatalf("setting up the union a second time: %v", err)
	}
	run(t, work, "git", "rebase", "origin/main")
	got := run(t, work, "git", "show", "HEAD:CLAUDE.md")
	for _, want := range []string{"one learned this", "two learned that"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the rebased CLAUDE.md to keep %q, got:\n%s", want, got)
		}
	}
	attributes := run(t, work, "git", "rev-parse", "--git-path", "info/attributes")
	if !filepath.IsAbs(attributes) {
		attributes = filepath.Join(work, attributes)
	}
	body, err := os.ReadFile(attributes)
	if err != nil {
		t.Fatalf("reading info/attributes: %v", err)
	}
	if n := strings.Count(string(body), "CLAUDE.md merge=union"); n != 1 {
		t.Errorf("expected the rule exactly once, found %d in:\n%s", n, body)
	}
}

func TestRefHeadNamesTheCommitARefPointsAt(t *testing.T) {
	here, _ := aRig(t)
	want := run(t, here, "git", "rev-parse", "origin/main")
	got, err := rig.New().RefHead(context.Background(), here, "origin/main")
	if err != nil || got != want {
		t.Errorf("expected %s, got %q, %v", want, got, err)
	}
	if _, err := rig.New().RefHead(context.Background(), here, "origin/nothing-here"); err == nil {
		t.Error("expected a ref that is not there to be an error")
	}
}

// TestACancelledPushKillsWhateverGitStarted checks a push that is cancelled
// takes the whole process group with it: git runs a transport helper
// underneath, and one left behind would keep holding the remote.
func TestACancelledPushKillsWhateverGitStarted(t *testing.T) {
	dir := t.TempDir()
	mainPidFile := filepath.Join(dir, "main.pid")
	childPidFile := filepath.Join(dir, "child.pid")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %q
( while true; do sleep 0.05; done ) &
echo $! > %q
wait
`, mainPidFile, childPidFile)
	program := filepath.Join(dir, "git")
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in for git: %v", err)
	}
	worktrees := rig.New(rig.WithProgram(program))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worktrees.Push(ctx, t.TempDir(), "origin", "main") }()

	mainPid := pidWhenWritten(t, mainPidFile)
	childPid := pidWhenWritten(t, childPidFile)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancelled push to report an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the cancelled push to return within a few seconds, not hang")
	}

	deadline := time.Now().Add(3 * time.Second)
	for (syscall.Kill(mainPid, 0) == nil || syscall.Kill(childPid, 0) == nil) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if syscall.Kill(mainPid, 0) == nil {
		t.Errorf("expected the git stand-in (pid %d) to be gone once cancelled", mainPid)
	}
	if syscall.Kill(childPid, 0) == nil {
		t.Errorf("expected the child it started (pid %d) to be gone too, not orphaned", childPid)
	}
}

// pidWhenWritten polls for a pid file a stand-in writes once it has started.
func pidWhenWritten(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("reading pid from %s: %v", path, err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return 0
}

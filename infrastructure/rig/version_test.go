package rig_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

const (
	packageAt = "{\n  \"name\": \"fixture\",\n  \"version\": \"%s\"\n}\n"
	lockAt    = "{\n  \"name\": \"fixture\",\n  \"version\": \"%s\",\n  \"lockfileVersion\": 3,\n  \"packages\": {\n    \"\": {\n      \"name\": \"fixture\",\n      \"version\": \"%s\"\n    }\n  }\n}\n"
)

// raiseTo writes both version files at version in dir and commits them.
func raiseTo(t *testing.T, dir, version, message string) {
	t.Helper()
	write(t, dir, "package.json", strings.Replace(packageAt, "%s", version, 1))
	write(t, dir, "package-lock.json", strings.ReplaceAll(lockAt, "%s", version))
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", message)
}

// aStoryAndTwoPatchBumps is a rig at 0.1.0 whose story branch touches only its
// own file, and whose main has since had two patch bumps landed by mw.
func aStoryAndTwoPatchBumps(t *testing.T) (here string) {
	t.Helper()
	here, _ = aRig(t)
	raiseTo(t, here, "0.1.0", "The rig opens at 0.1.0")
	run(t, here, "git", "push", "-q", "origin", "main")

	run(t, here, "git", "checkout", "-q", "-b", "mw/mw-x.1")
	write(t, here, "story.md", "the story's work\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The story's work")
	run(t, here, "git", "checkout", "-q", "main")

	raiseTo(t, here, "0.1.1", "Version 0.1.1 (mw-a.1)")
	raiseTo(t, here, "0.1.2", "Version 0.1.2 (mw-b.1)")
	run(t, here, "git", "push", "-q", "origin", "main")
	return here
}

func bumpedFrom(t *testing.T, here string, minor bool) (application.Bumped, string) {
	t.Helper()
	ctx := context.Background()
	worktrees := rig.New()
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	t.Cleanup(func() { _ = worktrees.CloseLanding(ctx, here, dir) })
	merged, err := worktrees.Merge(ctx, dir, "mw/mw-x.1")
	if err != nil {
		t.Fatalf("merging the story: %v", err)
	}
	bumped, err := worktrees.BumpVersion(ctx, dir, application.VersionBump{
		Files: []string{"package.json", "package-lock.json"}, Before: merged.Before, Branch: "mw/mw-x.1", StoryID: "mw-x.1", Minor: minor,
	})
	if err != nil {
		t.Fatalf("raising the version: %v", err)
	}
	return bumped, dir
}

func TestAMinorBumpRaisesTheMinorAfterTheMergeWhenMainMovedByPatchBumps(t *testing.T) {
	here := aStoryAndTwoPatchBumps(t)
	bumped, dir := bumpedFrom(t, here, true)
	if bumped.Version != "0.2.0" || bumped.Commit == "" {
		t.Fatalf("expected 0.2.0 in a commit, got %+v", bumped)
	}
	for _, file := range []string{"package.json", "package-lock.json"} {
		if text := run(t, dir, "git", "show", "HEAD:"+file); strings.Contains(text, "0.1.2") || !strings.Contains(text, `"version": "0.2.0"`) {
			t.Errorf("expected %s at 0.2.0 only, got:\n%s", file, text)
		}
	}
	if got := run(t, dir, "git", "log", "-1", "--format=%s"); strings.TrimSpace(got) != "Version 0.2.0 (mw-x.1)" {
		t.Errorf("expected the tip to say Version 0.2.0 (mw-x.1), got %q", got)
	}
}

func TestAPatchBumpStillRaisesThePatch(t *testing.T) {
	bumped, _ := bumpedFrom(t, aStoryAndTwoPatchBumps(t), false)
	if bumped.Version != "0.1.3" {
		t.Fatalf("expected 0.1.3, got %+v", bumped)
	}
}

func TestAStoryThatRaisedTheVersionByHandIsLeftAloneEvenWithABumpKey(t *testing.T) {
	here, _ := aRig(t)
	raiseTo(t, here, "0.1.0", "The rig opens at 0.1.0")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/mw-x.1")
	raiseTo(t, here, "0.2.0", "Version 0.2.0 (mw-x.1)")
	run(t, here, "git", "checkout", "-q", "main")
	run(t, here, "git", "fetch", "-q", "origin")

	bumped, dir := bumpedFrom(t, here, true)
	if bumped.Version != "" || bumped.Commit != "" {
		t.Fatalf("expected mw to add nothing, got %+v", bumped)
	}
	if text := run(t, dir, "git", "show", "HEAD:package.json"); !strings.Contains(text, `"version": "0.2.0"`) {
		t.Errorf("expected the story's own 0.2.0 to stand, got:\n%s", text)
	}
}

// A minor raised by hand in the story's branch is what sent mw-ht6gsw.5 back:
// the branch and main both changed the version line.
func TestAMinorRaisedByHandConflictsWithPatchBumpsOnMain(t *testing.T) {
	here, _ := aRig(t)
	raiseTo(t, here, "0.1.0", "The rig opens at 0.1.0")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/mw-x.1")
	raiseTo(t, here, "0.2.0", "Version 0.2.0 (mw-x.1)")
	run(t, here, "git", "checkout", "-q", "main")
	raiseTo(t, here, "0.1.1", "Version 0.1.1 (mw-a.1)")
	run(t, here, "git", "push", "-q", "origin", "main")

	ctx := context.Background()
	worktrees := rig.New()
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	defer worktrees.CloseLanding(ctx, here, dir)
	if _, err := worktrees.Merge(ctx, dir, "mw/mw-x.1"); !application.Conflicted(err) {
		t.Fatalf("expected the hand-raised minor to conflict with main's patch bump, got %v", err)
	}
}

// aStoryThatRaisedTheVersionByHandAndMainThatRaisedItToo is mw-vvrix1t: the
// story's branch took the rig from 0.1.0 to 0.1.1 itself and, while it waited,
// another landing took main to 0.1.1 as well, so the merge is clean and holds
// 0.1.1 for two different builds.
func aStoryThatRaisedTheVersionByHandAndMainThatRaisedItToo(t *testing.T, mainVersion string) (here string) {
	t.Helper()
	here, _ = aRig(t)
	raiseTo(t, here, "0.1.0", "The rig opens at 0.1.0")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/mw-x.1")
	raiseTo(t, here, "0.1.1", "Version 0.1.1 (mw-x.1)")
	run(t, here, "git", "checkout", "-q", "main")
	raiseTo(t, here, mainVersion, "Version "+mainVersion+" (mw-a.1)")
	run(t, here, "git", "push", "-q", "origin", "main")
	return here
}

func TestAStoryWhoseHandRaisedVersionMainAlreadyHoldsIsRaisedOnceMore(t *testing.T) {
	here := aStoryThatRaisedTheVersionByHandAndMainThatRaisedItToo(t, "0.1.1")
	ctx := context.Background()
	worktrees := rig.New()
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	defer worktrees.CloseLanding(ctx, here, dir)
	merged, err := worktrees.Merge(ctx, dir, "mw/mw-x.1")
	if err != nil {
		t.Fatalf("merging the story: %v", err)
	}
	bumped, err := worktrees.BumpVersion(ctx, dir, application.VersionBump{
		Files: []string{"package.json", "package-lock.json"}, Before: merged.Before, Branch: "mw/mw-x.1", StoryID: "mw-x.1",
		Changelog: []string{"changelog.json", "CHANGELOG.md"},
		Entry:     domain.ChangelogEntry{Date: "2026-10-27", Story: "mw-x.1", Kind: "Fixed", Text: "The story's note."},
	})
	if err != nil {
		t.Fatalf("raising the version: %v", err)
	}
	if bumped.Version != "0.1.2" || bumped.Commit == "" {
		t.Fatalf("expected 0.1.2 in a commit, as main already holds 0.1.1, got %+v", bumped)
	}
	for _, file := range []string{"package.json", "package-lock.json"} {
		if text := run(t, dir, "git", "show", "HEAD:"+file); !strings.Contains(text, `"version": "0.1.2"`) {
			t.Errorf("expected %s at 0.1.2, got:\n%s", file, text)
		}
	}
	if got := run(t, dir, "git", "log", "-1", "--format=%s"); strings.TrimSpace(got) != "Version 0.1.2 (mw-x.1)" {
		t.Errorf("expected the tip to say Version 0.1.2 (mw-x.1), got %q", got)
	}
	text := run(t, dir, "git", "show", "HEAD:changelog.json")
	if !strings.Contains(text, `"version": "0.1.2"`) || !strings.Contains(text, `"story": "mw-x.1"`) || strings.Contains(text, `"version": "0.1.1"`) {
		t.Errorf("expected the story's note under 0.1.2 only, got:\n%s", text)
	}
}

func TestAStoryWhoseHandRaisedVersionIsAboveMainsIsLeftAlone(t *testing.T) {
	here, _ := aRig(t)
	raiseTo(t, here, "0.1.0", "The rig opens at 0.1.0")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/mw-x.1")
	raiseTo(t, here, "0.1.2", "Version 0.1.2 (mw-x.1)")
	run(t, here, "git", "checkout", "-q", "main")
	write(t, here, "other.md", "another landing's work\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Another landing")
	run(t, here, "git", "push", "-q", "origin", "main")

	bumped, dir := bumpedFrom(t, here, false)
	if bumped.Version != "" || bumped.Commit != "" {
		t.Fatalf("expected mw to add nothing, got %+v", bumped)
	}
	if text := run(t, dir, "git", "show", "HEAD:package.json"); !strings.Contains(text, `"version": "0.1.2"`) {
		t.Errorf("expected the story's own 0.1.2 to stand, got:\n%s", text)
	}
}

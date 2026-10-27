package rig_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// An attempt's branch rebuilt after an earlier attempt pushed it: origin's copy
// is not an ancestor of the head. KeepOrigin merges it in keeping the head's
// files, dated no later than the head, and the plain push is then a
// fast-forward (mw-gq6.818).
func TestKeepOriginMergesInAnOriginCopyThatIsNotAnAncestorKeepingTheFiles(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	// Attempt 2 pushed mw/story.
	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "work.md", "attempt two\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Attempt two")
	run(t, here, "git", "push", "-q", "origin", "mw/story")
	pushed := run(t, here, "git", "rev-parse", "HEAD")

	// Attempt 3 starts again from main and writes different files.
	run(t, here, "git", "checkout", "-q", "-B", "mw/story", "main")
	write(t, here, "work.md", "attempt three\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Attempt three", "--date", "2020-01-01T00:00:00Z")
	head := run(t, here, "git", "rev-parse", "HEAD")
	headDate := run(t, here, "git", "log", "-1", "--format=%cI")
	before := run(t, here, "git", "rev-parse", "HEAD^{tree}")

	if err := worktrees.Push(ctx, here, "origin", "mw/story"); err == nil {
		t.Fatal("expected the plain push of a rebuilt branch to be refused")
	}

	kept, err := worktrees.KeepOrigin(ctx, here, "origin", "mw/story")
	if err != nil {
		t.Fatalf("KeepOrigin: %v", err)
	}
	if !kept.Merged || kept.Remote != pushed || kept.Head == head {
		t.Fatalf("expected origin's %s merged in on a new head, got %+v (head was %s)", pushed, kept, head)
	}
	if now := run(t, here, "git", "rev-parse", "HEAD"); now != kept.Head {
		t.Errorf("expected the worktree at %s, it is at %s", kept.Head, now)
	}
	if after := run(t, here, "git", "rev-parse", "HEAD^{tree}"); after != before {
		t.Errorf("expected the files kept, the tree went from %s to %s", before, after)
	}
	if got := strings.TrimSpace(run(t, here, "git", "show", "HEAD:work.md")); got != "attempt three" {
		t.Errorf("expected the attempt's file, got %q", got)
	}
	run(t, here, "git", "merge-base", "--is-ancestor", pushed, "HEAD")
	date, err := time.Parse(time.RFC3339, run(t, here, "git", "log", "-1", "--format=%cI"))
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := time.Parse(time.RFC3339, headDate); date.After(want) {
		t.Errorf("expected the merge dated no later than the head (%s), got %s", want, date)
	}
	if err := worktrees.Push(ctx, here, "origin", "mw/story"); err != nil {
		t.Fatalf("expected the push to fast-forward now, got %v", err)
	}
}

// A branch the remote lacks, or holds at an ancestor, is left exactly as it is.
func TestKeepOriginLeavesABranchTheRemoteLacksOrHoldsAtAnAncestor(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "work.md", "one\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "One")
	head := run(t, here, "git", "rev-parse", "HEAD")

	kept, err := worktrees.KeepOrigin(ctx, here, "origin", "mw/story")
	if err != nil || kept.Merged || kept.Remote != "" || kept.Head != head {
		t.Fatalf("expected a branch the remote lacks left alone, got %+v and %v", kept, err)
	}

	run(t, here, "git", "push", "-q", "origin", "mw/story")
	write(t, here, "work.md", "two\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Two")
	head = run(t, here, "git", "rev-parse", "HEAD")

	kept, err = worktrees.KeepOrigin(ctx, here, "origin", "mw/story")
	if err != nil || kept.Merged || kept.Head != head || kept.Remote == "" {
		t.Fatalf("expected a branch the remote holds at an ancestor left alone, got %+v and %v", kept, err)
	}
	if now := run(t, here, "git", "rev-parse", "HEAD"); now != head {
		t.Errorf("expected the head untouched, it is %s", now)
	}
}

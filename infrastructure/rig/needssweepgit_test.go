package rig_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// merged merges a branch of one commit into main the way a landing does.
func merged(t *testing.T, dir, bead string) string {
	t.Helper()
	run(t, dir, "git", "checkout", "-qb", "mw/"+bead)
	write(t, dir, bead+".txt", bead)
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-qm", "work for "+bead)
	run(t, dir, "git", "checkout", "-q", "main")
	run(t, dir, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'mw/"+bead+"' into HEAD", "mw/"+bead)
	return run(t, dir, "git", "rev-parse", "HEAD")
}

// tipOf is the tip of the story's branch: the commit a close reason names.
func tipOf(t *testing.T, dir, bead string) string {
	t.Helper()
	return run(t, dir, "git", "rev-parse", "mw/"+bead)
}

func TestRevertOfNamesTheRevertOfAMergeAndNoneForAMergeThatStands(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	merged(t, here, "mw-x.1")
	gone := merged(t, here, "mw-x.2")
	run(t, here, "git", "revert", "-m", "1", "--no-edit", gone)
	revert := run(t, here, "git", "rev-parse", "HEAD")

	w := rig.New()
	got, err := w.RevertOf(ctx, here, "main", gone, "mw-x.2")
	if err != nil || got != revert {
		t.Fatalf("RevertOf(reverted) = %q, %v; want %q", got, err, revert)
	}
	// The close reason names the branch's tip; it is the merge that was reverted.
	if got, err = w.RevertOf(ctx, here, "main", tipOf(t, here, "mw-x.2"), "mw-x.2"); err != nil || got != revert {
		t.Fatalf("RevertOf(tip) = %q, %v; want %q", got, err, revert)
	}
	// A short commit is read as the full one.
	if got, err = w.RevertOf(ctx, here, "main", gone[:9], "mw-x.2"); err != nil || got != revert {
		t.Fatalf("RevertOf(short) = %q, %v; want %q", got, err, revert)
	}
	if got, err = w.RevertOf(ctx, here, "main", tipOf(t, here, "mw-x.1"), "mw-x.1"); err != nil || got != "" {
		t.Fatalf("RevertOf(standing) = %q, %v; want none", got, err)
	}
	if _, err = w.RevertOf(ctx, here, "main", "0123456789abcdef", "mw-x.3"); err == nil {
		t.Fatal("a commit the checkout lacks must be an error, not no revert")
	}
}

func TestRevertOfIsNoneWhenTheRevertWasRevertedOrTheBranchMergedAgain(t *testing.T) {
	here, _ := aRig(t)
	ctx := context.Background()
	w := rig.New()

	undone := merged(t, here, "mw-y.1")
	run(t, here, "git", "revert", "-m", "1", "--no-edit", undone)
	first := run(t, here, "git", "rev-parse", "HEAD")
	run(t, here, "git", "revert", "--no-edit", first)
	if got, err := w.RevertOf(ctx, here, "main", undone, "mw-y.1"); err != nil || got != "" {
		t.Fatalf("a reverted revert: RevertOf = %q, %v; want none", got, err)
	}

	again := merged(t, here, "mw-y.2")
	run(t, here, "git", "revert", "-m", "1", "--no-edit", again)
	run(t, here, "git", "checkout", "-q", "mw/mw-y.2")
	write(t, here, "mw-y.2-fixed.txt", "fixed")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "fix again")
	run(t, here, "git", "checkout", "-q", "main")
	run(t, here, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'mw/mw-y.2' into HEAD", "mw/mw-y.2")
	if got, err := w.RevertOf(ctx, here, "main", again, "mw-y.2"); err != nil || got != "" {
		t.Fatalf("a later merge of the branch: RevertOf = %q, %v; want none", got, err)
	}
}

func TestMainRefsListsOriginsMainFirstAndSkipsWhatIsNotThere(t *testing.T) {
	here, _ := aRig(t)
	refs, err := rig.New().MainRefs(context.Background(), here)
	if err != nil || len(refs) != 2 || refs[0].Name != "origin/main" || refs[1].Name != "main" || refs[0].Sha != refs[1].Sha {
		t.Fatalf("MainRefs = %+v, %v", refs, err)
	}
}

func TestRevertOfReadsTheRemoteTrackingFormOfTheMergeMessage(t *testing.T) {
	here, _ := aRig(t)
	run(t, here, "git", "checkout", "-qb", "mw/mw-z.1")
	write(t, here, "z.txt", "z")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "work for mw-z.1")
	run(t, here, "git", "checkout", "-q", "main")
	run(t, here, "git", "merge", "-q", "--no-ff", "-m", "Merge remote-tracking branch 'origin/mw/mw-z.1' into HEAD", "mw/mw-z.1")
	merge := run(t, here, "git", "rev-parse", "HEAD")
	run(t, here, "git", "revert", "-m", "1", "--no-edit", merge)
	revert := run(t, here, "git", "rev-parse", "HEAD")
	w := rig.New()
	if got, err := w.RevertOf(context.Background(), here, "main", tipOf(t, here, "mw-z.1"), "mw-z.1"); err != nil || got != revert {
		t.Fatalf("RevertOf = %q, %v; want %q", got, err, revert)
	}
	// Another story whose id begins the same is not caught by it.
	if got, err := w.RevertOf(context.Background(), here, "main", run(t, here, "git", "rev-parse", "HEAD~2"), "mw-z.10"); err != nil || got != "" {
		t.Fatalf("RevertOf(other) = %q, %v; want none", got, err)
	}
}

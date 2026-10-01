package rig_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestBuiltMarksRememberTheLastCommitBuiltPerRig(t *testing.T) {
	ctx := context.Background()
	marks := rig.NewBuiltMarks(t.TempDir() + "/state")

	if got, err := marks.Built(ctx, "millwright"); err != nil || got != "" {
		t.Fatalf("expected no commit for a rig never built, got %q, %v", got, err)
	}
	if err := marks.MarkBuilt(ctx, "millwright", "abc123"); err != nil {
		t.Fatalf("marking: %v", err)
	}
	if err := marks.MarkBuilt(ctx, "elsewhere", "def456"); err != nil {
		t.Fatalf("marking another rig: %v", err)
	}
	if got, _ := marks.Built(ctx, "millwright"); got != "abc123" {
		t.Errorf("expected abc123, got %q", got)
	}
	if err := marks.MarkBuilt(ctx, "millwright", "fff999"); err != nil {
		t.Fatalf("marking again: %v", err)
	}
	if got, _ := marks.Built(ctx, "millwright"); got != "fff999" {
		t.Errorf("expected the newer mark to replace the older, got %q", got)
	}
}

func TestTipAndHeadReadTheRemoteBranchAndTheCheckout(t *testing.T) {
	here, other := aRig(t)
	ctx := context.Background()
	worktrees := rig.New()

	head, err := worktrees.Head(ctx, here)
	if err != nil {
		t.Fatalf("reading the head: %v", err)
	}
	write(t, other, "fix.md", "a fix\n")
	run(t, other, "git", "add", "-A")
	run(t, other, "git", "commit", "-qm", "A fix")
	run(t, other, "git", "push", "-q", "origin", "main")

	// Not fetched yet: the tip is still what the checkout last saw.
	if tip, err := worktrees.Tip(ctx, here, "origin", "main"); err != nil || tip != head {
		t.Fatalf("expected the old tip %s before a fetch, got %q, %v", head, tip, err)
	}
	if err := worktrees.Fetch(ctx, here); err != nil {
		t.Fatalf("fetching: %v", err)
	}
	want := run(t, other, "git", "rev-parse", "HEAD")
	if tip, err := worktrees.Tip(ctx, here, "origin", "main"); err != nil || tip != want {
		t.Errorf("expected the tip %s after a fetch, got %q, %v", want, tip, err)
	}
	if _, err := worktrees.Tip(ctx, here, "origin", "no-such-branch"); err == nil {
		t.Error("expected an error for a branch the remote does not have")
	}
}

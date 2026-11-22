package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1632: a story whose branch is not at the origin, whose commits are signed
// as a machine's, or that cannot be merged for a reason other than a conflict, was
// set aside by every batch and never handed back: only a conflict was counted
// (mw-gq6.1631). Each reason is counted the same way, and the batch that sets a
// story aside for the second time hands it back to a Builder.

// aBranchNotAtTheOrigin is a landing whose origin lacks the branches named.
type aBranchNotAtTheOrigin struct {
	*aBranchConflictLanding
	missing map[string]bool
}

func (l aBranchNotAtTheOrigin) RefHead(ctx context.Context, dir, ref string) (string, error) {
	if l.missing[ref] {
		return "", fmt.Errorf("git rev-parse --verify --quiet %s^{commit}: exit status 1", ref)
	}
	return l.aBranchConflictLanding.RefHead(ctx, dir, ref)
}

// aBranchThatWillNotMerge is a landing whose merge of the branches named fails
// with something that is not a conflict.
type aBranchThatWillNotMerge struct {
	*aBranchConflictLanding
	broken map[string]bool
}

func (l aBranchThatWillNotMerge) Merge(ctx context.Context, dir, branch string) (application.Landed, error) {
	if l.broken[branch] {
		return application.Landed{}, fmt.Errorf("merging %s: fatal: refusing to merge unrelated histories", branch)
	}
	return l.aBranchConflictLanding.Merge(ctx, dir, branch)
}

func TestAStoryWhoseBranchIsNotAtTheOriginIsHandedBackAfterTwoBatches(t *testing.T) {
	const id = "mw-e.1"
	a := aBatchOf(t, 3)
	a = aQueuedClaim(t, a)
	a.land.Next.Landing = aBranchNotAtTheOrigin{a.landing, map[string]bool{"origin/mw/" + id: true}}

	a.run(t)
	if got := a.tracker.State(id, application.RunState); got != application.RunQueued {
		t.Fatalf("expected %s still queued after its first set-aside, got run=%q", id, got)
	}
	if got := a.tracker.Metadata(id)[application.BatchConflictsField]; got != "1" {
		t.Errorf("expected the set-aside counted on the story (%s=1), got %q", application.BatchConflictsField, got)
	}

	second := aLaterBatch(a, 596)
	second.run(t)
	if got := second.tracker.State(id, application.RunState); got == application.RunQueued {
		t.Fatalf("expected %s taken off the batch queue after its second set-aside, got run=%q", id, got)
	}
	detail, err := second.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "" || detail.Status != application.StatusOpen {
		t.Errorf("expected the claim on %s given back, the story open for a Builder, got status %q held by %q", id, detail.Status, detail.Assignee)
	}
	comments := strings.Join(second.tracker.Comments(id), "\n---\n")
	for _, want := range []string{"origin/mw/" + id, "is not at the rig's origin", "Handed back"} {
		if !strings.Contains(comments, want) {
			t.Errorf("expected a comment on %s naming %q, got:\n%s", id, want, comments)
		}
	}
	if joined := strings.Join(second.report.Notes, "\n"); !strings.Contains(joined, id+" was handed back") {
		t.Errorf("expected a note in the report that %s was handed back, got %q", id, second.report.Notes)
	}
}

func TestAStoryThatCannotBeMergedForAnotherReasonIsHandedBackAfterTwoBatches(t *testing.T) {
	const id = "mw-e.1"
	a := aBatchOf(t, 3)
	a = aQueuedClaim(t, a)
	a.land.Next.Landing = aBranchThatWillNotMerge{a.landing, map[string]bool{"origin/mw/" + id: true}}

	a.run(t)
	if got := a.tracker.State(id, application.RunState); got != application.RunQueued {
		t.Fatalf("expected %s still queued after its first set-aside, got run=%q", id, got)
	}
	second := aLaterBatch(a, 596)
	second.run(t)
	if got := second.tracker.State(id, application.RunState); got == application.RunQueued {
		t.Fatalf("expected %s taken off the batch queue after its second set-aside, got run=%q", id, got)
	}
}

// A story set aside once for a reason other than a conflict stays queued.
func TestAStoryWhoseBranchIsNotAtTheOriginStaysQueuedAfterOneBatch(t *testing.T) {
	a := aBatchOf(t, 3)
	a = aQueuedClaim(t, a)
	a.land.Next.Landing = aBranchNotAtTheOrigin{a.landing, map[string]bool{"origin/mw/mw-e.2": true}}
	a.run(t)
	if got := a.tracker.State("mw-e.2", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-e.2 left queued after one set-aside, got run=%q", got)
	}
}

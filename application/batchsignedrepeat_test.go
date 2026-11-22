package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1633: a story whose commits are signed as a machine's work is set aside by
// a batch, which never merges it, and it stayed run=queued, so every batch took it
// and set it aside again, for ever. The second batch to set it aside for that hands
// it back to a Builder to reword the commits, and says which commit.

// aSignedBranchLanding is aConflictInFiles whose branch of the stories named in
// signed carries a commit signed as a machine's work.
type aSignedBranchLanding struct {
	aConflictInFiles
	signed map[string]bool
}

func (l aSignedBranchLanding) Commits(_ context.Context, _, branch, _ string) ([]application.Commit, error) {
	if l.signed[branch] {
		return []application.Commit{
			{Hash: "abc1234", Message: "Do the work\n\nCo-Authored-By: Some Model <noreply@example.com>"},
		}, nil
	}
	return nil, nil
}

func aSignedBatchOf(t *testing.T, n int, signed ...string) aSetAsideBatch {
	t.Helper()
	a := aQueuedClaim(t, aBatchOf(t, n))
	marks := map[string]bool{}
	for _, id := range signed {
		marks["origin/mw/"+id] = true
	}
	a.land.Next.Landing = aSignedBranchLanding{aConflictInFiles{a.landing}, marks}
	return a
}

func TestAStoryWithSignedCommitsIsHandedBackAfterTwoBatches(t *testing.T) {
	a := aSignedBatchOf(t, 3, "mw-e.1")
	const id = "mw-e.1"

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
		t.Fatalf("expected %s taken off the batch queue after its second set-aside for a signed commit, got run=%q", id, got)
	}
	detail, err := second.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "" || detail.Status != application.StatusOpen {
		t.Errorf("expected the claim on %s given back, the story open for a Builder, got status %q held by %q", id, detail.Status, detail.Assignee)
	}
	comments := strings.Join(second.tracker.Comments(id), "\n---\n")
	for _, want := range []string{"abc1234", "Co-Authored-By", "reword"} {
		if !strings.Contains(comments, want) {
			t.Errorf("expected a comment on %s naming %q, got:\n%s", id, want, comments)
		}
	}
	if strings.Contains(comments, "does not merge into") {
		t.Errorf("expected no word of a conflict in the comment on %s, got:\n%s", id, comments)
	}
	if joined := strings.Join(second.report.Notes, "\n"); !strings.Contains(joined, id+" was handed back") {
		t.Errorf("expected a note in the report that %s was handed back, got %q", id, second.report.Notes)
	}
}

// A story with one signed set-aside stays queued, and one that is clean carries no count.
func TestAStoryWithSignedCommitsStaysQueuedAfterOneBatchAndACleanOneIsNotCounted(t *testing.T) {
	a := aSignedBatchOf(t, 2, "mw-e.1")
	a.run(t)
	if got := a.tracker.State("mw-e.1", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-e.1 left queued after one set-aside, got run=%q", got)
	}
	if got := a.tracker.Metadata("mw-e.2")[application.BatchConflictsField]; got != "" {
		t.Errorf("expected no count on mw-e.2, which merged, got %q", got)
	}
}

package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1631: lampas stories mw-5r3p30.1231 and .1221 were set aside for a merge
// conflict by batches 622 and 623, as by one before, and stayed run=queued, so
// every batch took them, conflicted and set them aside again, for ever. A batch
// that gets a story with the conflict it was set aside for before hands it back
// to a Builder to rebase, not queued, and says which files conflicted.

// aConflictInFiles is aBranchConflictLanding whose conflict names its files, as the
// real landing's does (MergeConflict).
type aConflictInFiles struct {
	*aBranchConflictLanding
}

func (l aConflictInFiles) Merge(ctx context.Context, dir, branch string) (application.Landed, error) {
	landed, err := l.aBranchConflictLanding.Merge(ctx, dir, branch)
	if err != nil {
		return landed, &application.MergeConflict{
			Files: []string{"src/handsFree/session.ts", "docs/layout/reader.md"},
			Said:  "merging " + branch + ": CONFLICT (content): Merge conflict in src/handsFree/session.ts\nCONFLICT (content): Merge conflict in docs/layout/reader.md",
		}
	}
	return landed, nil
}

// aQueuedClaim makes each story of the batch what mw land reads: claimed, and run=queued.
func aQueuedClaim(t *testing.T, a aSetAsideBatch) aSetAsideBatch {
	t.Helper()
	ctx := context.Background()
	for _, s := range a.report.Queue {
		if err := a.tracker.ClaimStory(ctx, s.ID); err != nil {
			t.Fatal(err)
		}
		if err := a.tracker.SetStoryState(ctx, s.ID, application.RunState, application.RunQueued, "queued"); err != nil {
			t.Fatal(err)
		}
	}
	a.land.Next.Landing = aConflictInFiles{a.landing}
	return a
}

// aLaterBatch is the next batch over the same queue: a new report, the same stories.
func aLaterBatch(a aSetAsideBatch, number int) aSetAsideBatch {
	later := *a.report
	later.Number, later.Merged, later.SetAside, later.Notes = number, nil, nil, nil
	later.Landed, later.Held, later.Fixer = false, "", nil
	a.report = &later
	return a
}

func TestAStorySetAsideForAConflictTwiceInARowIsHandedBackToBeRebasedNotQueuedAgain(t *testing.T) {
	a := aBatchOf(t, 3, "mw-e.1")
	a = aQueuedClaim(t, a)
	const id = "mw-e.1"

	a.run(t)
	if got := a.tracker.State(id, application.RunState); got != application.RunQueued {
		t.Fatalf("expected %s still queued after its first set-aside, which the next batch may find merging now, got run=%q", id, got)
	}

	second := aLaterBatch(a, 596)
	second.run(t)
	if got := second.tracker.State(id, application.RunState); got == application.RunQueued {
		t.Fatalf("expected %s taken off the batch queue after its second set-aside for a conflict, got run=%q", id, got)
	}
	detail, err := second.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "" || detail.Status != application.StatusOpen {
		t.Errorf("expected the claim on %s given back, the story open for a Builder, got status %q held by %q", id, detail.Status, detail.Assignee)
	}
	comments := strings.Join(second.tracker.Comments(id), "\n---\n")
	for _, want := range []string{"src/handsFree/session.ts", "docs/layout/reader.md", "rebase"} {
		if !strings.Contains(comments, want) {
			t.Errorf("expected a comment on %s naming %q, got:\n%s", id, want, comments)
		}
	}
	if joined := strings.Join(second.report.Notes, "\n"); !strings.Contains(joined, id+" was handed back") {
		t.Errorf("expected a note in the report that %s was handed back, got %q", id, second.report.Notes)
	}
	for _, other := range []string{"mw-e.2", "mw-e.3"} {
		if got := second.tracker.State(other, application.RunState); got != application.RunQueued && !second.report.Landed {
			t.Errorf("expected %s untouched, got run=%q", other, got)
		}
	}
}

// A story set aside for a conflict that has not conflicted before is not handed back:
// the next batch may find it merging, once what it conflicted with has landed.
func TestAStorySetAsideForAConflictOnceStaysQueued(t *testing.T) {
	a := aBatchOf(t, 3, "mw-e.2")
	a = aQueuedClaim(t, a)
	a.run(t)

	if got := a.tracker.State("mw-e.2", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-e.2 left queued after one set-aside, got run=%q", got)
	}
	detail, _ := a.tracker.ShowStory(context.Background(), "mw-e.2")
	if detail.Assignee == "" {
		t.Errorf("expected the claim on mw-e.2 kept after one set-aside")
	}
	if got := a.tracker.Metadata("mw-e.2")[application.BatchConflictsField]; got != "1" {
		t.Errorf("expected the set-aside counted on the story (%s=1), got %q", application.BatchConflictsField, got)
	}
}

// A story that merges is never counted.
func TestAStoryThatMergesCarriesNoCountOfSetAsides(t *testing.T) {
	a := aBatchOf(t, 2, "mw-e.2")
	a = aQueuedClaim(t, a)
	a.run(t)
	if got := a.tracker.Metadata("mw-e.1")[application.BatchConflictsField]; got != "" {
		t.Errorf("expected no count on mw-e.1, which merged, got %q", got)
	}
}

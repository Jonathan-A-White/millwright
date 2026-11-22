package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1636: the finding that a story whose branch is not at the rig's origin,
// or whose merge fails for a reason other than a conflict, is set aside by every
// batch with no count was met by mw-gq6.1632. This is the sweep of every s.Why
// assignment in BatchLand.mergeAll that is not a conflict: whatever the reason, a
// story set aside twice is handed back to a Builder, with the reason in the comment
// and no word of a conflict.
func TestAStorySetAsideForAnyOtherReasonTwiceIsHandedBack(t *testing.T) {
	const id = "mw-e.1"
	cases := []struct {
		name  string
		batch func(t *testing.T) aSetAsideBatch
		want  []string
	}{
		{
			name: "its branch is not at the origin",
			batch: func(t *testing.T) aSetAsideBatch {
				a := aQueuedClaim(t, aBatchOf(t, 3))
				a.land.Next.Landing = aBranchNotAtTheOrigin{a.landing, map[string]bool{"origin/mw/" + id: true}}
				return a
			},
			want: []string{"is not at the rig's origin"},
		},
		{
			name: "its commits are signed as a machine's",
			batch: func(t *testing.T) aSetAsideBatch {
				return aSignedBatchOf(t, 3, id)
			},
			want: []string{"abc1234", "Co-Authored-By"},
		},
		{
			name: "its merge fails for another reason",
			batch: func(t *testing.T) aSetAsideBatch {
				a := aQueuedClaim(t, aBatchOf(t, 3))
				a.land.Next.Landing = aBranchThatWillNotMerge{a.landing, map[string]bool{"origin/mw/" + id: true}}
				return a
			},
			want: []string{"it could not be merged", "refusing to merge unrelated histories"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := c.batch(t)
			a.run(t)
			if got := a.tracker.State(id, application.RunState); got != application.RunQueued {
				t.Fatalf("expected %s still queued after its first set-aside, got run=%q", id, got)
			}
			if got := a.tracker.Metadata(id)[application.BatchConflictsField]; got != "1" {
				t.Errorf("expected the first set-aside counted on the story (%s=1), got %q", application.BatchConflictsField, got)
			}

			second := aLaterBatch(a, 596)
			second.run(t)
			if got := second.tracker.State(id, application.RunState); got == application.RunQueued {
				t.Fatalf("expected %s handed back after its second set-aside, got run=%q", id, got)
			}
			detail, err := second.tracker.ShowStory(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if detail.Assignee != "" || detail.Status != application.StatusOpen {
				t.Errorf("expected the claim on %s given back and the story open, got status %q held by %q", id, detail.Status, detail.Assignee)
			}
			comments := strings.Join(second.tracker.Comments(id), "\n---\n")
			for _, want := range append([]string{"Handed back"}, c.want...) {
				if !strings.Contains(comments, want) {
					t.Errorf("expected a comment on %s naming %q, got:\n%s", id, want, comments)
				}
			}
			if strings.Contains(comments, "does not merge into") {
				t.Errorf("expected no word of a conflict in the comment on %s, got:\n%s", id, comments)
			}
			if joined := strings.Join(second.report.Notes, "\n"); !strings.Contains(joined, id+" was handed back") {
				t.Errorf("expected a note that %s was handed back, got %q", id, second.report.Notes)
			}
		})
	}
}

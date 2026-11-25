package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.1735: a batch that took a story before mw retry gave it a new attempt
// still acted on it at the end: it wrote "stays queued" on a story that was no
// longer run=queued, and handBack cleared the new attempt's run state and
// released its claim. A batch that finds the story no longer queued when it
// comes to act on it leaves it alone.

// aRetryDuringTheMerge is aConflictInFiles whose merge of a branch is the moment
// mw retry gives the story a new attempt: its run state is replaced by running.
type aRetryDuringTheMerge struct {
	aConflictInFiles
	retried func(context.Context)
}

func (l aRetryDuringTheMerge) Merge(ctx context.Context, dir, branch string) (application.Landed, error) {
	landed, err := l.aConflictInFiles.Merge(ctx, dir, branch)
	if err != nil && dir != trialDir && l.retried != nil {
		l.retried(ctx)
	}
	return landed, err
}

func TestABatchLeavesAloneAStoryRetriedWhileItRan(t *testing.T) {
	const id = "mw-e.2"
	for _, tc := range []struct {
		name  string
		batch func(t *testing.T) aSetAsideBatch
	}{
		{name: "set aside the first time, with the others landing", batch: func(t *testing.T) aSetAsideBatch {
			a := aBatchOf(t, 3, id)
			a.landing.onlyBesideMates(id)
			return aQueuedClaim(t, a)
		}},
		{name: "set aside the second time, which hands it back", batch: func(t *testing.T) aSetAsideBatch {
			a := aBatchOf(t, 3, id)
			a.landing.onlyBesideMates(id)
			a = aQueuedClaim(t, a)
			a.run(t)
			return aLaterBatch(a, 596)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.batch(t)
			ctx := context.Background()
			before := len(a.tracker.Comments(id))
			a.land.Next.Landing = aRetryDuringTheMerge{aConflictInFiles{a.landing}, func(ctx context.Context) {
				if err := a.tracker.ClearRunState(ctx, id); err != nil {
					t.Fatal(err)
				}
				if err := a.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"); err != nil {
					t.Fatal(err)
				}
			}}
			a.run(t)

			if got := a.tracker.State(id, application.RunState); got != application.RunRunning {
				t.Errorf("expected the new attempt's run state %q left on %s, got %q", application.RunRunning, id, got)
			}
			detail, err := a.tracker.ShowStory(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if detail.Assignee == "" {
				t.Errorf("expected the new attempt's claim on %s left alone, got a story held by nobody (status %q)", id, detail.Status)
			}
			if got := a.tracker.Comments(id); len(got) != before {
				t.Errorf("expected no comment written on %s by the batch, got %q", id, got[before:])
			}
			if joined := strings.Join(a.report.Notes, "\n"); strings.Contains(joined, id+" was handed back") {
				t.Errorf("expected no note that %s was handed back, got %q", id, a.report.Notes)
			}
			for _, s := range a.report.SetAside {
				if s.ID == id && s.HandedBack {
					t.Errorf("expected %s not marked handed back", id)
				}
			}
		})
	}
}

// The sweep of the same fault: a story held red alone for the second time, whose
// new attempt began while the batch's tests ran, is left alone too.
func TestABatchLeavesAloneAStoryRetriedWhileItRanRedAlone(t *testing.T) {
	const id = "mw-e.1"
	a, _ := aRedAloneBatch(t, aHeldRedAloneLine(594))
	ctx := context.Background()
	a.land.Next.Checks = aRetryDuringTheTests{a.checks, func() {
		if err := a.tracker.ClearRunState(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := a.tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "retried"); err != nil {
			t.Fatal(err)
		}
	}}
	a.run(t)

	if got := a.tracker.State(id, application.RunState); got != application.RunRunning {
		t.Errorf("expected the new attempt's run state %q left on %s, got %q", application.RunRunning, id, got)
	}
	if got := a.tracker.Comments(id); len(got) != 0 {
		t.Errorf("expected no comment written on %s by the batch, got %q", id, got)
	}
	if len(a.report.RedAloneBack) != 0 {
		t.Errorf("expected %s not handed back, got %+v", id, a.report.RedAloneBack)
	}
}

// aRetryDuringTheTests is the scripted checks with mw retry happening as they run.
type aRetryDuringTheTests struct {
	*scriptedChecks
	retried func()
}

func (c aRetryDuringTheTests) Run(ctx context.Context, dir, command string) (application.Checked, error) {
	c.retried()
	return c.scriptedChecks.Run(ctx, dir, command)
}

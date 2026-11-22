package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1631 found that a story held red alone is taken again by every batch
// when nothing else is queued, held again and mailed again, with no hand-back
// (mw-gq6.1634). A batch of one story whose tests are red, when the story was
// held red alone by a batch since the rig last landed, hands it back to a Builder.

// aRedAloneBatch is a batch of the one story mw-e.1 whose tests are red, over
// a history that holds the lines given.
func aRedAloneBatch(t *testing.T, before ...application.BatchMetric) (aSetAsideBatch, *apptest.FakeBatchMetrics) {
	t.Helper()
	a := aBatchOf(t, 1)
	a.land.Fix = false
	a.checks.passes = []bool{false}
	a = aQueuedClaim(t, a)
	history := &apptest.FakeBatchMetrics{}
	for _, line := range before {
		mustDo(t, history.AppendBatch(context.Background(), line))
	}
	a.land.Metrics = history
	return a, history
}

func aHeldRedAloneLine(batch int) application.BatchMetric {
	return application.BatchMetric{Kind: application.BatchKind, At: time.Date(2026, 11, 20, 10, batch%60, 0, 0, time.UTC),
		Rig: "millwright", Batch: batch, Stories: 1, IDs: []string{"mw-e.1"}, Outcome: application.BatchHeld, Red: true}
}

func TestAStoryHeldRedAloneTwiceIsHandedBackToABuilder(t *testing.T) {
	const id = "mw-e.1"
	a, _ := aRedAloneBatch(t, aHeldRedAloneLine(594))
	a.run(t)

	if a.report.Landed || a.report.Held == "" {
		t.Fatalf("expected the batch held, got landed %v, held %q", a.report.Landed, a.report.Held)
	}
	if got := a.tracker.State(id, application.RunState); got == application.RunQueued {
		t.Fatalf("expected %s taken off the batch queue after the second batch held it red alone, got run=%q", id, got)
	}
	detail, err := a.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "" || detail.Status != application.StatusOpen {
		t.Errorf("expected the claim on %s given back, the story open for a Builder, got status %q held by %q", id, detail.Status, detail.Assignee)
	}
	comments := strings.Join(a.tracker.Comments(id), "\n---\n")
	for _, want := range []string{"Handed back", "red", "batch 594"} {
		if !strings.Contains(comments, want) {
			t.Errorf("expected a comment on %s naming %q, got:\n%s", id, want, comments)
		}
	}
	if joined := strings.Join(a.report.Notes, "\n"); !strings.Contains(joined, id+" was handed back") {
		t.Errorf("expected a note in the report that %s was handed back, got %q", id, a.report.Notes)
	}
}

// The first batch that holds a story red alone leaves it queued: the main it
// was red on may have been mended since.
func TestAStoryHeldRedAloneOnceStaysQueued(t *testing.T) {
	const id = "mw-e.1"
	a, _ := aRedAloneBatch(t)
	a.run(t)
	if a.report.Held == "" {
		t.Fatalf("expected the batch held, got landed %v", a.report.Landed)
	}
	if got := a.tracker.State(id, application.RunState); got != application.RunQueued {
		t.Errorf("expected %s left queued after one batch held it red alone, got run=%q", id, got)
	}
}

// aLandedLine is a batch of the rig that landed the stories given.
func aLandedLine(batch int, ids ...string) application.BatchMetric {
	return application.BatchMetric{Kind: application.BatchKind, At: time.Date(2026, 11, 20, 10, batch%60, 0, 0, time.UTC),
		Rig: "millwright", Batch: batch, Stories: len(ids), IDs: ids, Outcome: application.BatchGreen}
}

// mw-gq6.1637: a story held red alone, left out while others landed, and taken
// alone later started its count again, because the count stopped at the rig's
// last landing, so it could be held red alone every time the queue emptied.
func TestAStoryHeldRedAloneAcrossALandingIsHandedBack(t *testing.T) {
	const id = "mw-e.1"
	a, _ := aRedAloneBatch(t, aHeldRedAloneLine(594), aLandedLine(595, "mw-e.9"))
	a.run(t)

	if got := a.tracker.State(id, application.RunState); got == application.RunQueued {
		t.Fatalf("expected %s handed back after it was held red alone again across a landing, got run=%q", id, got)
	}
	comments := strings.Join(a.tracker.Comments(id), "\n---\n")
	for _, want := range []string{"Handed back", "batch 594"} {
		if !strings.Contains(comments, want) {
			t.Errorf("expected a comment on %s naming %q, got:\n%s", id, want, comments)
		}
	}
}

// A landing of the story itself, or a hand-back of it, starts its count again:
// what was red before it was mended is not what is red now.
func TestAStoryLandedOrHandedBackAfterBeingHeldRedAloneStartsItsCountAgain(t *testing.T) {
	const id = "mw-e.1"
	handed := aHeldRedAloneLine(595)
	handed.HandedBack = []string{id}
	for name, after := range map[string]application.BatchMetric{
		"landed":     aLandedLine(595, id),
		"handedback": handed,
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := aRedAloneBatch(t, aHeldRedAloneLine(594), after)
			a.run(t)
			if got := a.tracker.State(id, application.RunState); got != application.RunQueued {
				t.Errorf("expected %s left queued, its count started again, got run=%q", id, got)
			}
		})
	}
}

func TestHeldRedAloneBeforeReadsAcrossLandingsOfOtherStories(t *testing.T) {
	lines := []application.BatchMetric{aHeldRedAloneLine(1), aLandedLine(2, "mw-x.1"), aHeldRedAloneLine(3)}
	if batch, ok := application.HeldRedAloneBefore(lines, "mw-e.1"); !ok || batch != 3 {
		t.Errorf("expected batch 3, got %d, %v", batch, ok)
	}
	if _, ok := application.HeldRedAloneBefore(lines, "mw-x.1"); ok {
		t.Errorf("expected no hold for a story never held red alone")
	}
}

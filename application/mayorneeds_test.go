package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mayorNeedsFor reads what the Mayor's hands need are over every bead labelled
// hitl the tracker has ready.
func mayorNeedsFor(t *testing.T, tracker *apptest.FakeTracker) []application.PosternViewNeed {
	t.Helper()
	hitl, err := tracker.ReadyWithLabel(context.Background(), application.LabelHitl)
	if err != nil {
		t.Fatalf("listing the hitl beads: %v", err)
	}
	needs, err := application.MayorReader{
		Tracker: tracker, Notes: tracker, Now: func() time.Time { return statusNow },
	}.MayorNeeds(context.Background(), hitl)
	if err != nil {
		t.Fatalf("reading the Mayor's needs: %v", err)
	}
	return needs
}

func TestMayorReaderListsAHandsBeadWithNoStepAsWaitingOnTheMayor(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.70", time.Hour)

	needs := mayorNeedsFor(t, tracker)

	if len(needs) != 1 || needs[0].Bead != "mw-gq6.70" || needs[0].WaitsFor != application.PosternWaitsMayor {
		t.Fatalf("expected the one hands bead waiting on the Mayor, got %+v", needs)
	}
	if note, _ := tracker.Note(context.Background(), application.PosternSnapshotMemoryKey); note != "" {
		t.Fatalf("expected no landed memory written, got %q", note)
	}
}

func TestMayorReaderLeavesOutWhatTheViewMakesSomeoneElsesNeed(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.71", 4*24*time.Hour) // stale: the view asks him
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.72", time.Hour)
	if err := tracker.CommentOnStory(context.Background(), "mw-gq6.72", "BY HAND: run the script"); err != nil {
		t.Fatalf("commenting: %v", err)
	}
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.73", time.Hour)
	if err := tracker.SetLabels("mw-gq6.73", application.LabelHitl, application.LabelDemo); err != nil {
		t.Fatalf("labelling: %v", err)
	}
	aHandsBeadFiledAgo(t, tracker, "mw-gq6.74", time.Hour)
	if err := tracker.SetNote(context.Background(), application.PosternKeepKey("mw-gq6.74"),
		statusNow.Add(time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatalf("keeping: %v", err)
	}

	if needs := mayorNeedsFor(t, tracker); len(needs) != 0 {
		t.Fatalf("expected none of them to wait on the Mayor, got %+v", needs)
	}
}

package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// causeListTracker is a tracker holding two stories in two rigs, each named a
// cause by mw cause, and a third that is named none.
func causeListTracker(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("e-a", domain.Path{Rig: "alpha"})
	tracker.AddEpic("e-b", domain.Path{Rig: "beta"})
	tracker.AddStory("e-a", domain.Story{ID: "mw-a1", Title: "alpha story"})
	tracker.AddStory("e-b", domain.Story{ID: "mw-b1", Title: "beta story"})
	tracker.AddStory("e-b", domain.Story{ID: "mw-b2", Title: "beta story never refused"})
	tracker.AddStory("e-a", domain.Story{ID: "mw-fix", Title: "the mend"})
	cause := application.Cause{Tracker: tracker}
	if _, err := cause.Run(context.Background(), "mw-a1", "tooling", "the  gate\ntimed out", "mw-fix"); err != nil {
		t.Fatal(err)
	}
	if _, err := cause.Run(context.Background(), "mw-b1", "code", "wrong branch taken", ""); err != nil {
		t.Fatal(err)
	}
	return tracker
}

// TestCauseListNamesEachCausedStoryOnALine is mw-gq6.778: the loop's Clerk reads
// which story got which class of cause, not a count per rig.
func TestCauseListNamesEachCausedStoryOnALine(t *testing.T) {
	var out bytes.Buffer
	list := application.CauseList{Tracker: causeListTracker(t), Since: time.Time{}, Out: &out}
	rows, err := list.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected the two caused stories, got %+v", rows)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one line a story, got %q", out.String())
	}
	for i, want := range []string{
		"mw-a1 · alpha · tooling · the gate timed out · mw-fix",
		"mw-b1 · beta · code · wrong branch taken · -",
	} {
		if lines[i] != want {
			t.Errorf("line %d: expected %q, got %q", i, want, lines[i])
		}
	}
}

// TestCauseListRigNarrowsToOne: --rig keeps only that rig's stories.
func TestCauseListRigNarrowsToOne(t *testing.T) {
	var out bytes.Buffer
	list := application.CauseList{Tracker: causeListTracker(t), Rig: "beta", Out: &out}
	rows, err := list.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].StoryID != "mw-b1" {
		t.Fatalf("expected only beta's mw-b1, got %+v", rows)
	}
	if strings.Contains(out.String(), "mw-a1") || !strings.Contains(out.String(), "mw-b1") {
		t.Errorf("expected only mw-b1 printed, got %q", out.String())
	}
}

// TestCauseListSaysSoWhenNothingWasNamed: an empty window prints a line, not nothing.
func TestCauseListSaysSoWhenNothingWasNamed(t *testing.T) {
	var out bytes.Buffer
	list := application.CauseList{Tracker: apptest.NewFakeTracker(), Out: &out}
	if _, err := list.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no cause") {
		t.Errorf("expected a line saying no cause was named, got %q", out.String())
	}
}

// TestLoopBriefCausesNamesTheListCommand: the brief's CAUSES part points at mw cause list.
func TestLoopBriefCausesNamesTheListCommand(t *testing.T) {
	brief := application.ReadLoopBrief(application.LoopInputs{})
	if text := brief.String(); !strings.Contains(text, "mw cause list") {
		t.Errorf("expected CAUSES to name mw cause list, got %q", text)
	}
}

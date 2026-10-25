package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// handsStepChildReport is an mw status over a tracker holding the epic mw-hs
// with one open child, mw-hs.1, and, when step is set, a hands step on the epic
// that has not run (mw-gq6.763).
func handsStepChildReport(t *testing.T, step string) application.StatusReport {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-hs", domain.Path{})
	addHandsStepChild(t, tracker)
	if step != "" {
		if err := tracker.SetNote(context.Background(), application.HandsStepsKey("mw-hs"), step); err != nil {
			t.Fatal(err)
		}
	}
	report, err := application.Status{
		Tracker: tracker,
		Notes:   tracker,
		Host:    "laptop",
		Seat:    "builder",
		Now:     func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return report
}

// A ready child of an epic with a hands step not yet run shows the epic's step
// beside it, so a Mayor does not dispatch it unawares.
func TestStatusShowsTheHandsStepChildOfAnEpicWithALiveStep(t *testing.T) {
	report := handsStepChildReport(t, `[{"id":"swap","host":"laptop","as":"user","run":"true","way_back":"true","added_at":"2026-10-24T00:00:00Z"}]`)
	text := report.String()
	if !strings.Contains(text, "mw-hs.1") {
		t.Fatalf("the child is not listed ready:\n%s", text)
	}
	if !strings.Contains(text, "epic mw-hs has a hands step not yet run: swap") {
		t.Errorf("the epic's live hands step is not shown beside its child:\n%s", text)
	}
}

// A ready child of an epic with no hands step has no such line.
func TestStatusShowsNoHandsStepChildOfAnEpicWithNoStep(t *testing.T) {
	text := handsStepChildReport(t, "").String()
	if !strings.Contains(text, "mw-hs.1") {
		t.Fatalf("the child is not listed ready:\n%s", text)
	}
	if strings.Contains(text, "hands step") {
		t.Errorf("a hands step is named for an epic with none:\n%s", text)
	}
}

// A step that ran clean is no longer live.
func TestStatusShowsNoHandsStepChildOfAnEpicWhoseStepRan(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-hs", domain.Path{})
	addHandsStepChild(t, tracker)
	ctx := context.Background()
	if err := tracker.SetNote(ctx, application.HandsStepsKey("mw-hs"), `[{"id":"swap","host":"laptop","as":"user","run":"true","way_back":"true","added_at":"2026-10-24T00:00:00Z"}]`); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(ctx, application.HandsRanKey("mw-hs", "swap"), `{"at":"2026-10-24T01:00:00Z","exit":0,"host":"laptop"}`); err != nil {
		t.Fatal(err)
	}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
		Now: func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if text := report.String(); strings.Contains(text, "hands step") {
		t.Errorf("a step that ran clean is shown as live:\n%s", text)
	}
}

// addHandsStepChild files the open child mw-hs.1 under mw-hs, pathed to the
// laptop so that host finds it ready.
func addHandsStepChild(t *testing.T, tracker *apptest.FakeTracker) {
	t.Helper()
	story := domain.Story{ID: "mw-hs.1", Title: "Child of the epic"}
	if err := story.Overrides.Set("host", "laptop"); err != nil {
		t.Fatal(err)
	}
	tracker.AddStory("mw-hs", story)
}

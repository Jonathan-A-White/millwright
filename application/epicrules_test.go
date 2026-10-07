package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestAMapOfARigIsNotHeldToTheRigsEpicRequirements(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	path := domain.Path{Rig: "spell-forge", Branch: "main", Harness: "claude", Model: "opus", Effort: "high", Host: "vps"}
	for _, id := range []string{"map-1", "epic-1"} {
		tracker.AddEpic(id, path)
		tracker.DescribeEpicText(id, "Cast spells.")
	}
	if err := tracker.AddLabel(context.Background(), "map-1", "wayfinder:map"); err != nil {
		t.Fatalf("labelling the map: %v", err)
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("spell-forge", domain.EpicRequirements{Sections: []string{"Demo"}, LastStoryLabels: []string{"demo"}})

	report, err := application.Status{
		Tracker: tracker,
		Notes:   tracker,
		Rules:   rules,
		Host:    "vps",
		Seat:    "builder",
		Now:     func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}

	if len(report.EpicShortfalls) != 1 || report.EpicShortfalls[0].ID != "epic-1" {
		t.Fatalf("want only epic-1 reported, got %+v", report.EpicShortfalls)
	}
	if got := len(report.EpicShortfalls[0].Missing); got != 2 {
		t.Errorf("epic-1 should miss the Demo section and the demo story, got %+v", report.EpicShortfalls[0].Missing)
	}
}

package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw status's LAND RUSH section (plans/0117, Land Rush 6): one line for each
// rig that lands in batches.

const landRushNow = "2026-10-15 14:00"

func landRushStatus(t *testing.T, mode domain.LandingMode, batches ...application.BatchMetric) string {
	t.Helper()
	ctx := context.Background()
	now, _ := time.Parse("2006-01-02 15:04", landRushNow)
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("lp-1", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "laptop"})
	for id, ago := range map[string]time.Duration{"lp-1.1": 42 * time.Minute, "lp-1.2": 10 * time.Minute} {
		tracker.AddStory("lp-1", domain.Story{ID: id, Title: "The story " + id})
		mustDo(t, tracker.ClaimStory(ctx, id))
		queue(t, tracker, id)
		mustDo(t, tracker.SetStoryMetadata(ctx, id, map[string]string{application.QueuedAtField: now.Add(-ago).Format(time.RFC3339)}))
	}
	rules := apptest.NewFakeEpicRules()
	rules.Require("lampas", domain.EpicRequirements{Landing: domain.LandingSettings{Mode: mode}})
	log := &apptest.FakeBatchMetrics{}
	for _, b := range batches {
		mustDo(t, log.AppendBatch(ctx, b))
	}
	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
		Rigs:  map[string]string{"lampas": "/rigs/lampas"},
		Rules: rules, Batches: log,
		Now: func() time.Time { return now },
	}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return report.String()
}

func batchAt(n int, hhmm, outcome string, stories int) application.BatchMetric {
	at, _ := time.Parse("2006-01-02 15:04", "2026-10-15 "+hhmm)
	return application.BatchMetric{Kind: application.BatchKind, At: at, Rig: "lampas", Batch: n, Stories: stories, Outcome: outcome, GateRuns: 1}
}

func TestStatusShowsALandRushLineForABatchRig(t *testing.T) {
	out := landRushStatus(t, domain.LandingBatch,
		batchAt(1, "11:00", application.BatchGreen, 4),
		batchAt(2, "12:30", application.BatchGreen, 3),
	)
	for _, want := range []string{"LAND RUSH\n", "lampas", "2 queued", "oldest 42m", "last batch 12:30", "3 stories", "green", "none held"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in:\n%s", want, out)
		}
	}
}

func TestStatusCountsTheBatchesHeldInARow(t *testing.T) {
	out := landRushStatus(t, domain.LandingBatch,
		batchAt(1, "09:00", application.BatchHeld, 4),
		batchAt(2, "10:00", application.BatchFixed, 4),
		batchAt(3, "11:00", application.BatchHeld, 2),
		batchAt(4, "12:00", application.BatchHeld, 5),
	)
	if !strings.Contains(out, "last batch 12:00") || !strings.Contains(out, "5 stories, held") || !strings.Contains(out, "2 held in a row") {
		t.Fatalf("expected the last batch held and two held in a row in:\n%s", out)
	}
}

func TestStatusSaysNoBatchYetForABatchRigThatHasNone(t *testing.T) {
	out := landRushStatus(t, domain.LandingBatch)
	if !strings.Contains(out, "no batch yet") || !strings.Contains(out, "2 queued") {
		t.Fatalf("expected a line with 2 queued and no batch yet in:\n%s", out)
	}
}

func TestStatusHasNoLandRushLineForARigThatLandsEach(t *testing.T) {
	for _, mode := range []domain.LandingMode{domain.LandingEach, ""} {
		out := landRushStatus(t, mode, batchAt(1, "11:00", application.BatchGreen, 4))
		if strings.Contains(out, "LAND RUSH") {
			t.Fatalf("a rig in mode %q has no LAND RUSH line, got:\n%s", mode, out)
		}
	}
}

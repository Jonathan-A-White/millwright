package application_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// The line a batch leaves in metrics/factory.jsonl (plans/0117, section 3):
// the stop rule is read off these.

func TestALandedBatchIsAMetricLineWithTheStopRuleFields(t *testing.T) {
	at := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	report := application.BatchReport{
		Rig: "lampas", Host: "laptop", Number: 7, Landed: true, Tested: true,
		Merged:   []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}, {ID: "lp-3"}},
		SetAside: []application.BatchStory{{ID: "lp-4"}},
		GateTook: 14*time.Minute + 30*time.Second,
	}
	line, err := json.Marshal(report.Metric(at))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"kind": "batch", "rig": "lampas", "host": "laptop", "batch": 7.0, "stories": 3.0, "set_aside": 1.0,
		"gate_runs": 1.0, "gate_minutes": 14.5, "fixer": false, "fixer_tokens": 0.0,
		"outcome": "green", "red_minutes": 0.0, "at": "2026-10-15T13:00:00Z",
	}
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s: expected %v, got %v in %s", key, w, got[key], line)
		}
	}
	if _, dated := got["day"]; dated {
		t.Errorf("a batch line must not carry a day, or the daily snapshots would read it: %s", line)
	}
}

func TestAHeldBatchIsAMetricLineWithTheOutcomeHeld(t *testing.T) {
	report := application.BatchReport{
		Rig: "lampas", Number: 2, Tested: true, Held: "red",
		Queue:    []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}},
		Merged:   []application.BatchStory{{ID: "lp-1"}, {ID: "lp-2"}},
		GateTook: 10 * time.Minute,
	}
	m := report.Metric(time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC))
	if m.Outcome != application.BatchHeld || m.Stories != 2 || m.GateRuns != 1 || m.GateMinutes != 10 {
		t.Fatalf("expected a held batch of 2 stories, one gate run of 10 minutes, got %+v", m)
	}
}

func TestABatchHeldBeforeTheGateRanHasNoGateRuns(t *testing.T) {
	report := application.BatchReport{Rig: "lampas", Number: 3, Held: "no branch merged", Queue: []application.BatchStory{{ID: "lp-1"}}}
	m := report.Metric(time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC))
	if m.GateRuns != 0 || m.GateMinutes != 0 || m.Outcome != application.BatchHeld || m.Stories != 1 {
		t.Fatalf("expected one story, no gate run and held, got %+v", m)
	}
}

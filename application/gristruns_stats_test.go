package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// timingsStore is a GristRunStore that holds fixture timings.
type timingsStore struct{ timings []application.GristRunTiming }

func (s timingsStore) Keep(context.Context, application.GristRun) error { return nil }
func (s timingsStore) List(context.Context, time.Time) ([]application.GristRunLine, error) {
	return nil, nil
}
func (s timingsStore) Timings(context.Context) ([]application.GristRunTiming, error) {
	return s.timings, nil
}

func fixtureTiming(kind string, at time.Time, queue, scoring, local, azure, harness float64) application.GristRunTiming {
	sent := at.Add(-time.Duration(queue * float64(time.Second)))
	t := application.GristRunTiming{
		Txid: "direct:" + at.Format("150405"), Kind: kind, Received: at, Sent: &sent,
		HarnessSeconds: harness, Seconds: scoring + harness,
	}
	if scoring > 0 {
		scored := at.Add(time.Duration(scoring * float64(time.Second)))
		t.ScoredAt = &scored
		t.ScoringSeconds = scoring
		t.Scorers = map[string]float64{"local": local, "azure": azure}
	}
	return t
}

func TestStatsPrintTheMedianAndWorstOfEachPhaseOfAKind(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	store := timingsStore{timings: []application.GristRunTiming{
		fixtureTiming("tutor-turn", at, 1, 2, 1, 2, 10),
		fixtureTiming("other", at.Add(time.Minute), 99, 99, 99, 99, 99),
		fixtureTiming("tutor-turn", at.Add(2*time.Minute), 3, 4, 2, 4, 20),
		fixtureTiming("tutor-turn", at.Add(3*time.Minute), 8, 6, 3, 6, 30),
	}}
	var out bytes.Buffer
	rows, err := application.GristStats{Runs: store, Out: &out}.Run(context.Background(), "tutor-turn", 20)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]float64{ // median, worst, count
		"queue": {3, 8, 3}, "scoring": {4, 6, 3}, "local": {2, 3, 3}, "azure": {4, 6, 3},
		"harness": {20, 30, 3}, "total": {24, 36, 3},
	}
	if len(rows) != len(want) {
		t.Fatalf("expected %d phases, got %+v", len(want), rows)
	}
	for _, r := range rows {
		w, ok := want[r.Phase]
		if !ok || r.Median != w[0] || r.Worst != w[1] || float64(r.Count) != w[2] {
			t.Errorf("phase %q: expected %v, got median %v worst %v count %d", r.Phase, w, r.Median, r.Worst, r.Count)
		}
	}
	for _, text := range []string{"tutor-turn", "phase", "median", "worst", "count", "queue", "scoring", "local", "azure", "harness", "total"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("expected %q in the table:\n%s", text, out.String())
		}
	}
}

func TestStatsTakeOnlyTheLastNRunsAndMedianOfAnEvenCountIsTheMiddlePairsMean(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	var all []application.GristRunTiming
	for i, h := range []float64{100, 1, 2, 3, 4} {
		all = append(all, fixtureTiming("tutor-turn", at.Add(time.Duration(i)*time.Minute), 0, 0, 0, 0, h))
	}
	rows, err := application.GristStats{Runs: timingsStore{all}}.Run(context.Background(), "tutor-turn", 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Phase == "harness" && (r.Median != 2.5 || r.Worst != 4 || r.Count != 4) {
			t.Errorf("expected harness median 2.5 worst 4 of 4, got %+v", r)
		}
	}
}

func TestStatsOfAKindWithNoRunsSaysSo(t *testing.T) {
	var out bytes.Buffer
	rows, err := application.GristStats{Runs: timingsStore{}, Out: &out}.Run(context.Background(), "tutor-turn", 20)
	if err != nil || len(rows) != 0 || !strings.Contains(out.String(), "no runs of tutor-turn") {
		t.Fatalf("expected a line saying so, got %v %v %q", rows, err, out.String())
	}
}

package application

import (
	"context"
	"fmt"
	"io"
	"sort"
)

// GristStatsLast is how many of a kind's latest runs mw grist stats looks at
// when it is not told.
const GristStatsLast = 20

// GristStatRow is one phase of a kind's runs: the median and the worst of its
// seconds, over the count of runs that had the phase.
type GristStatRow struct {
	Phase string
	// Scorer is set for an engine's row, which is part of scoring.
	Scorer        bool
	Median, Worst float64
	Count         int
}

// GristStats is `mw grist stats`: where the wait goes, by phase, over the last
// runs of one kind. Only kinds, times and ids are read.
type GristStats struct {
	Runs GristRunStore
	// Out is where the table is printed; nil prints nothing.
	Out io.Writer
}

// Run reports the phases of the last runs of kind: queue (sent to received),
// scoring and each engine within it, harness and total, in that order. A
// phase no run had is left out.
func (g GristStats) Run(ctx context.Context, kind string, last int) ([]GristStatRow, error) {
	if g.Runs == nil {
		return nil, fmt.Errorf("mw grist stats: nowhere the mill keeps its runs")
	}
	if last <= 0 {
		last = GristStatsLast
	}
	all, err := g.Runs.Timings(ctx)
	if err != nil {
		return nil, err
	}
	var timings []GristRunTiming
	for _, t := range all {
		if t.Kind == kind {
			timings = append(timings, t)
		}
	}
	if len(timings) > last {
		timings = timings[len(timings)-last:]
	}
	if len(timings) == 0 {
		if g.Out != nil {
			fmt.Fprintf(g.Out, "the mill has kept no runs of %s\n", kind)
		}
		return nil, nil
	}
	var queue, scoring, harness, total []float64
	engines := map[string][]float64{}
	for _, t := range timings {
		if t.Sent != nil {
			queue = append(queue, t.Received.Sub(*t.Sent).Seconds())
		}
		if t.ScoredAt != nil {
			scoring = append(scoring, t.ScoringSeconds)
		}
		for name, seconds := range t.Scorers {
			engines[name] = append(engines[name], seconds)
		}
		harness = append(harness, t.HarnessSeconds)
		total = append(total, t.Seconds)
	}
	var rows []GristStatRow
	add := func(phase string, seconds []float64) {
		if len(seconds) > 0 {
			rows = append(rows, statRow(phase, seconds))
		}
	}
	add("queue", queue)
	add("scoring", scoring)
	names := make([]string, 0, len(engines))
	for name := range engines {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add(name, engines[name])
		rows[len(rows)-1].Scorer = true
	}
	add("harness", harness)
	add("total", total)
	if g.Out != nil {
		fmt.Fprintf(g.Out, "%s: the last %d runs\n%-10s  %8s  %8s  %5s\n", kind, len(timings), "phase", "median", "worst", "count")
		for _, r := range rows {
			phase := r.Phase
			if r.Scorer {
				phase = "  " + phase
			}
			fmt.Fprintf(g.Out, "%-10s  %7.1fs  %7.1fs  %5d\n", phase, r.Median, r.Worst, r.Count)
		}
	}
	return rows, nil
}

func statRow(phase string, seconds []float64) GristStatRow {
	sorted := append([]float64(nil), seconds...)
	sort.Float64s(sorted)
	n := len(sorted)
	median := sorted[n/2]
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return GristStatRow{Phase: phase, Median: median, Worst: sorted[n-1], Count: n}
}

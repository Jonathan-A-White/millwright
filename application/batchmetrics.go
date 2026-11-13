package application

import (
	"context"
	"sort"
	"time"
)

// BatchKind marks a line of metrics/factory.jsonl as a batch's, not a day's:
// the daily key-metric snapshots carry a day and no kind, a batch line carries
// a kind and no day, so each reader skips the other's lines (plans/0117,
// section 3).
const BatchKind = "batch"

// The outcomes a batch line holds. A batch that landed is green; one a fixer
// had to mend before it landed is fixed (Land Rush 5's); one that pushed
// nothing is held.
const (
	BatchGreen = "green"
	BatchFixed = "fixed"
	BatchHeld  = "held"
)

// BatchMetric is the line a batch leaves in metrics/factory.jsonl, one for
// each batch of each rig, so that the trial's stop rule can be read off them
// (plans/0117, section 3): two held in a row and more than a quarter needing a
// fixer are read straight off them; tokens per landing and bugs per landing
// are read with the ledger's fuel and the nightly suite's bugs beside them.
type BatchMetric struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
	Rig  string    `json:"rig"`
	Host string    `json:"host"`
	// Batch is the number under runs/batch/<rig>/.
	Batch int `json:"batch"`
	// Stories are the stories a batch landed, or when it was held the stories
	// it tried to; SetAside those it left queued for a later batch.
	Stories  int `json:"stories"`
	SetAside int `json:"set_aside"`
	// GateRuns and GateMinutes are the runs of the rig's tests the batch made
	// and the minutes they took.
	GateRuns    int     `json:"gate_runs"`
	GateMinutes float64 `json:"gate_minutes"`
	// Fixer says a fixer was asked to mend the batch, and FixerTokens is the
	// fuel it spent. A batch with no fixer spent none.
	Fixer       bool   `json:"fixer"`
	FixerTokens int    `json:"fixer_tokens"`
	Outcome     string `json:"outcome"`
	// RedMinutes is how long the batch stood red: from its first red gate to
	// green or to held. A batch that never went red stood none.
	RedMinutes float64 `json:"red_minutes"`
	// IDs are the stories the batch landed, or when it was held those it
	// tried to; Red says its tests ran on it and failed at least once. The
	// follower and the next batch read them back (mw-gq6.1246).
	IDs []string `json:"ids,omitempty"`
	Red bool     `json:"red,omitempty"`
}

// Metric is the line the batch leaves, as of at.
func (r BatchReport) Metric(at time.Time) BatchMetric {
	m := BatchMetric{
		Kind: BatchKind, At: at.UTC(), Rig: r.Rig, Host: r.Host, Batch: r.Number,
		Stories: len(r.Queue), SetAside: len(r.SetAside), Outcome: BatchHeld,
	}
	if r.Landed {
		m.Stories, m.Outcome = len(r.Merged), BatchGreen
	}
	m.IDs, m.Red = storyIDs(r.Queue), !r.RedAt.IsZero()
	if r.Landed {
		m.IDs = storyIDs(r.Merged)
	}
	if r.Tested {
		m.GateRuns = max(r.GateRuns, 1)
		m.GateMinutes = r.GateTook.Minutes()
	}
	if f := r.Fixer; f != nil && f.Started {
		m.Fixer, m.FixerTokens = true, f.Result.Fuel.Total()
		if r.Landed {
			m.Outcome = BatchFixed
		}
	}
	if !r.RedAt.IsZero() {
		m.RedMinutes = at.Sub(r.RedAt).Minutes()
	}
	return m
}

// readBatches are the batch lines, oldest first as the history keeps them: a
// read that fails, or no history, is none.
func readBatches(ctx context.Context, history BatchHistory) []BatchMetric {
	if history == nil {
		return nil
	}
	lines, err := history.Batches(ctx)
	if err != nil {
		return nil
	}
	return lines
}

// rigBatches are the rig's lines of lines, oldest first.
func rigBatches(lines []BatchMetric, rig string) []BatchMetric {
	var kept []BatchMetric
	for _, line := range lines {
		if line.Rig == rig {
			kept = append(kept, line)
		}
	}
	// Two hosts' lines merge by union: the file's order is not always the time's.
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].At.Before(kept[j].At) })
	return kept
}

// heldRed says the batch was held after its tests ran red on it: the stories',
// not the host's, which holds a batch it never tested.
func (m BatchMetric) heldRed() bool { return m.Outcome == BatchHeld && m.Red }

// HeldRedTogether is the rig's last batch line when that batch was held red
// with more than one story in it: one story's fault held the rest, so its
// stories are landed again one at a time (mw-gq6.1246).
func HeldRedTogether(lines []BatchMetric) (BatchMetric, bool) {
	if len(lines) == 0 {
		return BatchMetric{}, false
	}
	last := lines[len(lines)-1]
	return last, last.heldRed() && len(last.IDs) > 1
}

// HeldRedAlone are the stories held red when landed alone since the rig last
// landed a batch, each with the number of the batch it was held in: what made
// a batch red, which the next is made without (mw-gq6.1246).
func HeldRedAlone(lines []BatchMetric) map[string]int {
	alone := map[string]int{}
	for i := len(lines) - 1; i >= 0 && lines[i].Outcome == BatchHeld; i-- {
		if line := lines[i]; line.heldRed() && len(line.IDs) == 1 {
			if _, seen := alone[line.IDs[0]]; !seen {
				alone[line.IDs[0]] = line.Batch
			}
		}
	}
	return alone
}

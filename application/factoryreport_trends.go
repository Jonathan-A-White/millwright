package application

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// ArrowBand is how far a number may move against the window before it, as a
// share of that number, and still read as level (PROVISIONAL).
const ArrowBand = 0.05

// The arrows, and the words that say whether the way a number moved is good.
const (
	ArrowUp       = "↑"
	ArrowDown     = "↓"
	ArrowLevel    = "↔"
	VerdictBetter = "better"
	VerdictWorse  = "worse"
)

// Trend is one number against the same number in the window before: that
// number, the arrow, and whether the way it moved is better or worse. The zero
// Trend says there was nothing to compare with.
type Trend struct {
	Previous float64 `json:"previous"`
	Arrow    string  `json:"arrow"`
	// Verdict is better or worse, and empty for a level number.
	Verdict string `json:"verdict,omitempty"`
}

// NewTrend compares now with previous inside the ArrowBand. lowerIsBetter fixes
// the sense of the arrow for the measure: a refusal rate that rises is worse, a
// count of landings that rises is better.
func NewTrend(now, previous float64, lowerIsBetter bool) Trend {
	t := Trend{Previous: previous, Arrow: ArrowLevel}
	if math.Abs(now-previous) <= ArrowBand*math.Abs(previous) {
		return t
	}
	up := now > previous
	t.Arrow = ArrowDown
	if up {
		t.Arrow = ArrowUp
	}
	t.Verdict = VerdictBetter
	if up == lowerIsBetter {
		t.Verdict = VerdictWorse
	}
	return t
}

// Text is the trend as it follows a number in a line: " ↔", " ↓ better", or
// nothing at all when there was nothing to compare with.
func (t Trend) Text() string {
	switch {
	case t.Arrow == "":
		return ""
	case t.Verdict == "":
		return " " + t.Arrow
	}
	return " " + t.Arrow + " " + t.Verdict
}

// trendIf is NewTrend, or the zero Trend when the window before had no basis.
func trendIf(basis bool, now, previous float64, lowerIsBetter bool) Trend {
	if !basis {
		return Trend{}
	}
	return NewTrend(now, previous, lowerIsBetter)
}

// Speed is how long the landings of one block and window took against their
// par: the median and p90 of actual over par, and the median wall time from
// dispatch to landing. It is read from the close-outs' benchmarks.
type Speed struct {
	// Measured is the landings that have a benchmark with a wall time, and
	// WithPar those of them held to a par.
	Measured int `json:"measured"`
	WithPar  int `json:"with_par"`
	// ParMedian and ParP90 are actual over par, 1 at par.
	ParMedian float64 `json:"par_median"`
	ParP90    float64 `json:"par_p90"`
	// DispatchToLandingS is the median wall time, in seconds.
	DispatchToLandingS float64 `json:"dispatch_to_landing_s"`
}

// FuelUse is what the landings of one block and window burned, per landing: the
// tokens their ledger rows counted and the dollars their result files report,
// every attempt of a story summed. A landing with no figure is left out of the
// mean rather than counted as free.
type FuelUse struct {
	Tokens           int     `json:"tokens"`
	WithTokens       int     `json:"landings_with_tokens"`
	TokensPerLanding float64 `json:"tokens_per_landing"`
	CostUSD          float64 `json:"cost_usd"`
	WithCost         int     `json:"landings_with_cost"`
	USDPerLanding    float64 `json:"usd_per_landing"`
}

// percentile is the nearest-rank percentile (p in 0..1) of values; 0 for none.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(0, min(rank, len(sorted)-1))]
}

// sumSpeed sums the landings among entries.
func sumSpeed(entries []reportEntry) Speed {
	var s Speed
	var ratios, actuals []float64
	for _, e := range entries {
		if !e.measured {
			continue
		}
		actuals = append(actuals, e.actualS)
		if e.ratio > 0 {
			ratios = append(ratios, e.ratio)
		}
	}
	s.Measured, s.WithPar = len(actuals), len(ratios)
	s.ParMedian = twoPlaces(median(ratios))
	s.ParP90 = twoPlaces(percentile(ratios, 0.9))
	s.DispatchToLandingS = math.Round(median(actuals))
	return s
}

// sumFuel sums the fuel of the landings among entries.
func sumFuel(entries []reportEntry) FuelUse {
	var f FuelUse
	for _, e := range entries {
		if !e.landed {
			continue
		}
		if e.tokens > 0 {
			f.Tokens += e.tokens
			f.WithTokens++
		}
		if e.costUSD > 0 {
			f.CostUSD += e.costUSD
			f.WithCost++
		}
	}
	if f.WithTokens > 0 {
		f.TokensPerLanding = math.Round(float64(f.Tokens) / float64(f.WithTokens))
	}
	if f.WithCost > 0 {
		f.CostUSD = twoPlaces(f.CostUSD)
		f.USDPerLanding = twoPlaces(f.CostUSD / float64(f.WithCost))
	}
	return f
}

// GateTrend is one host's gate on one rig, the usual against the latest, each
// against the same in the window before.
type GateTrend struct {
	Rig     string           `json:"rig"`
	Host    string           `json:"host"`
	UsualS  float64          `json:"usual_s"`
	LatestS float64          `json:"latest_s"`
	Arrows  map[string]Trend `json:"arrows"`
}

// gateTrends reads the gate lines of every benchmark so far against those of
// the benchmarks before the window began.
func gateTrends(benchmarks []Benchmark, since time.Time, rig, host string) []GateTrend {
	var before []Benchmark
	for _, b := range benchmarks {
		if b.At.Before(since) {
			before = append(before, b)
		}
	}
	type key struct{ rig, host string }
	earlier := map[key]GateLine{}
	for _, line := range GateLines(before, BenchmarkSettings{}) {
		earlier[key{line.Rig, line.Host}] = line
	}
	var out []GateTrend
	for _, line := range GateLines(benchmarks, BenchmarkSettings{}) {
		if (rig != "" && line.Rig != rig) || (host != "" && line.Host != host) {
			continue
		}
		g := GateTrend{Rig: line.Rig, Host: line.Host, UsualS: line.Usual.Seconds(), LatestS: line.Latest.Seconds(), Arrows: map[string]Trend{}}
		if was, ok := earlier[key{line.Rig, line.Host}]; ok {
			g.Arrows["usual"] = NewTrend(g.UsualS, was.Usual.Seconds(), true)
			g.Arrows["latest"] = NewTrend(g.LatestS, was.Latest.Seconds(), true)
		}
		out = append(out, g)
	}
	return out
}

// qualityArrows compares a block's quality with the window before; nothing at
// all when that window had nothing in it.
func qualityArrows(now, before Quality) map[string]Trend {
	arrows := map[string]Trend{}
	if before.Empty() {
		return arrows
	}
	set := func(name string, t Trend) {
		if t.Arrow != "" {
			arrows[name] = t
		}
	}
	set("landed", NewTrend(float64(now.Landed), float64(before.Landed), false))
	set("refused", NewTrend(float64(now.Refused), float64(before.Refused), true))
	set("refusal_rate", trendIf(before.Landed+before.Refused > 0, now.RefusalRate, before.RefusalRate, true))
	both := now.Landed > 0 && before.Landed > 0
	set("attempts_per_landing", trendIf(both, now.AttemptsPerLanding, before.AttemptsPerLanding, true))
	set("bugs_per_landing", trendIf(both, now.BugsPerLanding, before.BugsPerLanding, true))
	set("findings_per_landing", trendIf(both, now.FindingsPerLanding, before.FindingsPerLanding, true))
	set("escaped_bugs", NewTrend(float64(now.EscapedBugs), float64(before.EscapedBugs), true))
	return arrows
}

// speedArrows and fuelArrows compare a block's speed and fuel with the window
// before, for the measures both windows have.
func speedArrows(now, before Speed) map[string]Trend {
	arrows := map[string]Trend{}
	if now.WithPar > 0 && before.WithPar > 0 {
		arrows["par_median"] = NewTrend(now.ParMedian, before.ParMedian, true)
		arrows["par_p90"] = NewTrend(now.ParP90, before.ParP90, true)
	}
	if now.Measured > 0 && before.Measured > 0 {
		arrows["dispatch_to_landing"] = NewTrend(now.DispatchToLandingS, before.DispatchToLandingS, true)
	}
	return arrows
}

func fuelArrows(now, before FuelUse) map[string]Trend {
	arrows := map[string]Trend{}
	if now.WithTokens > 0 && before.WithTokens > 0 {
		arrows["tokens_per_landing"] = NewTrend(now.TokensPerLanding, before.TokensPerLanding, true)
	}
	if now.WithCost > 0 && before.WithCost > 0 {
		arrows["usd_per_landing"] = NewTrend(now.USDPerLanding, before.USDPerLanding, true)
	}
	return arrows
}

// vsBefore is the line that says how the numbers of a line moved: "  vs
// before: median ↑ worse, p90 ↓ better", or nothing when none has an arrow.
func vsBefore(indent string, labels []string, trends ...Trend) string {
	var parts []string
	for i, t := range trends {
		if t.Arrow != "" {
			parts = append(parts, labels[i]+t.Text())
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("%svs before: %s", indent, strings.Join(parts, ", "))
}

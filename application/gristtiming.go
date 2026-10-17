package application

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"time"
)

// gristNoticeKeep is how long GristNotices holds a notice nobody took: a
// grist noticed and never answered does not stay in memory.
const gristNoticeKeep = time.Hour

// GristNotices remembers when GristWatch noticed each grist at the backend, in
// the follower's own process, for the pass of the mill it springs to say how
// long the wake took. A pass the dispatch tick runs finds none and counts
// from its own start. The zero value is not ready; a nil *GristNotices notes
// and takes nothing.
type GristNotices struct {
	mu sync.Mutex
	at map[string]time.Time
}

// NewGristNotices is an empty set of notices.
func NewGristNotices() *GristNotices {
	return &GristNotices{at: map[string]time.Time{}}
}

// Note records that txid was noticed at the time given; the first notice of a
// txid stands. Notices older than gristNoticeKeep are dropped.
func (n *GristNotices) Note(txid string, at time.Time) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for old, when := range n.at {
		if at.Sub(when) > gristNoticeKeep {
			delete(n.at, old)
		}
	}
	if _, ok := n.at[txid]; !ok {
		n.at[txid] = at
	}
}

// Take reports when txid was noticed and forgets it; false when nothing
// noticed it.
func (n *GristNotices) Take(txid string) (time.Time, bool) {
	if n == nil {
		return time.Time{}, false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	at, ok := n.at[txid]
	delete(n.at, txid)
	return at, ok
}

// tenths is seconds rounded to one decimal, as a grind line keeps it.
func tenths(seconds float64) *float64 {
	rounded := math.Round(seconds*10) / 10
	return &rounded
}

// phase is the seconds from one moment to a later one, or nil when either
// moment is not known or they come the wrong way round (the backend's clock
// is not this host's).
func phase(from, to time.Time) *float64 {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return nil
	}
	return tenths(to.Sub(from).Seconds())
}

// GristCrossClockMax is the longest span between two moments on different
// clocks that is taken as measured: no sender waits longer than a grist's own
// timeout for its answer, so a longer span is a clock that is off, not a wait
// (PROVISIONAL, mw-gq6.510).
const GristCrossClockMax = DefaultGristTimeout

// crossClockSeconds is the seconds from one moment to a later one when the two
// were read on different clocks (the backend's or the phone's, and this
// host's): nil, not known, when either moment is not known, they come the wrong
// way round, or the span is over GristCrossClockMax. It is not clamped: a
// clamped number would read as measured.
func crossClockSeconds(from, to time.Time) *float64 {
	if from.IsZero() || to.IsZero() || to.Before(from) || to.Sub(from) > GristCrossClockMax {
		return nil
	}
	seconds := to.Sub(from).Seconds()
	return &seconds
}

// crossClockPhase is crossClockSeconds in tenths, as a grind line keeps it.
func crossClockPhase(from, to time.Time) *float64 {
	seconds := crossClockSeconds(from, to)
	if seconds == nil {
		return nil
	}
	return tenths(*seconds)
}

// GristTiming is `mw grist timing`: where the seconds of the mill's answers
// went, from the phases each grind line carries. Only the record's own
// numbers are read: no tokens, no network.
type GristTiming struct {
	State GristState
	// Out is where the table is printed; nil prints nothing.
	Out io.Writer
}

// GristTimingPhases is the name and meaning of each phase mw grist timing
// prints, in order: what --help says.
var GristTimingPhases = []struct{ Name, Means string }{
	{"seconds", "the mill's own time, from its grind starting (a refusal: the pass taking it up) to the answer delivered or kept; every line has it, old ones too"},
	{"wake", "the grist sent on the backend to noticed by the follower's watch (or the dispatch tick's pass)"},
	{"pickup", "noticed to the pass starting on the grist"},
	{"slot", "waiting for one of the mill's grind slots"},
	{"startup", "the harness process starting, to its own clock starting (the whole run when it reports none)"},
	{"model", "the rest of the harness run"},
	{"deliver", "the answer ready to the backend taking it"},
	{"total", "the grist sent to its answer delivered"},
	{"tap_to_sent", "the app's report (client_timing): the user's tap to the grist accepted by the backend, upload included"},
	{"delivered_to_seen", "the app's report: the answer delivered on the backend to the app's poll seeing it"},
	{"wait", "the app's report: delivered_to_seen less the poll's own round trip, the wait between checks (when the app sent poll_network)"},
	{"seen_to_shown", "the app's report: the answer seen to drawn on screen"},
	{"network", "the app's report: the round trip of the poll that saw the answer plus that of the request that sent it; inside tap_to_sent and delivered_to_seen, not added to them"},
	{"round_trip", "tap to shown: tap_to_sent + total + delivered_to_seen + seen_to_shown, where the app reported them"},
}

// GristTimingRow is one phase of one app and kind: how many grinds had it, and
// the median and 90th percentile of its seconds.
type GristTimingRow struct {
	Phase       string
	Count       int
	Median, P90 float64
}

// GristTimingGroup is one app and kind and its phases.
type GristTimingGroup struct {
	App, Kind string
	Grinds    int
	Rows      []GristTimingRow
}

// Run reads the record and reports, for each app and kind (by name),
// the count, median and p90 of every phase some grind had. since keeps only
// lines at or after since (the zero time keeps every line); app and kind, when
// not empty, keep those alone. A grist the harness would not grind for a limit
// of the factory's ends in about a second and is no grind's work: it is left
// out of every count and median, and the table ends in a line saying how many
// (mw-gq6.549).
func (g GristTiming) Run(ctx context.Context, since time.Time, app, kind string) ([]GristTimingGroup, error) {
	if g.State == nil {
		return nil, fmt.Errorf("mw grist timing: nowhere the mill keeps its record")
	}
	lines, err := g.State.Lines(ctx)
	if err != nil {
		return nil, err
	}
	reported, err := g.State.ClientTimings(ctx)
	if err != nil {
		return nil, err
	}
	leftOut := 0
	clients := map[string]GristClientTiming{}
	for _, c := range reported {
		if _, ok := clients[c.Txid]; !ok {
			clients[c.Txid] = c
		}
	}
	type key struct{ app, kind string }
	grouped := map[key][]GrindLine{}
	for _, line := range lines {
		if line.Time.Before(since) {
			continue
		}
		if (app != "" && line.App != app) || (kind != "" && line.Kind != kind) {
			continue
		}
		if IsGristReasonLimited(line.Reason) {
			leftOut++
			continue
		}
		grouped[key{line.App, line.Kind}] = append(grouped[key{line.App, line.Kind}], line)
	}
	var groups []GristTimingGroup
	for k, ls := range grouped {
		group := GristTimingGroup{App: k.app, Kind: k.kind, Grinds: len(ls)}
		for _, p := range GristTimingPhases {
			var seconds []float64
			for _, line := range ls {
				s := line.phaseSeconds(p.Name)
				if client, ok := clients[line.Txid]; ok && s == nil {
					s = client.phaseSeconds(p.Name, line)
				}
				if s != nil {
					seconds = append(seconds, *s)
				}
			}
			if len(seconds) > 0 {
				group.Rows = append(group.Rows, timingRow(p.Name, seconds))
			}
		}
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].App != groups[j].App {
			return groups[i].App < groups[j].App
		}
		return groups[i].Kind < groups[j].Kind
	})
	if g.Out != nil {
		g.print(groups, leftOut)
	}
	return groups, nil
}

// phaseSeconds is the seconds of the phase named, or nil when the line does not
// have it.
func (l GrindLine) phaseSeconds(name string) *float64 {
	switch name {
	case "seconds":
		return &l.Seconds
	case "wake":
		return l.WakeSeconds
	case "pickup":
		return l.PickupSeconds
	case "slot":
		return l.SlotSeconds
	case "startup":
		return l.StartupSeconds
	case "model":
		return l.ModelSeconds
	case "deliver":
		return l.DeliverSeconds
	case "total":
		return l.TotalSeconds
	}
	return nil
}

func (g GristTiming) print(groups []GristTimingGroup, leftOut int) {
	if leftOut > 0 {
		defer fmt.Fprintf(g.Out, "left out %d limited\n", leftOut)
	}
	if len(groups) == 0 {
		fmt.Fprintln(g.Out, "the mill's record holds no grinds that fit")
		return
	}
	for i, group := range groups {
		if i > 0 {
			fmt.Fprintln(g.Out)
		}
		name := group.App + " " + group.Kind
		if group.App == "" && group.Kind == "" {
			name = "(unread grists)"
		}
		noun := "grinds"
		if group.Grinds == 1 {
			noun = "grind"
		}
		fmt.Fprintf(g.Out, "%s: %d %s\n%-17s  %5s  %8s  %8s\n", name, group.Grinds, noun, "phase", "count", "median", "p90")
		for _, r := range group.Rows {
			fmt.Fprintf(g.Out, "%-17s  %5d  %7.1fs  %7.1fs\n", r.Phase, r.Count, r.Median, r.P90)
		}
	}
}

// timingRow is the median and nearest-rank 90th percentile of seconds.
func timingRow(phase string, seconds []float64) GristTimingRow {
	sorted := append([]float64(nil), seconds...)
	sort.Float64s(sorted)
	n := len(sorted)
	median := sorted[n/2]
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	rank := int(math.Ceil(0.9 * float64(n)))
	return GristTimingRow{Phase: phase, Count: n, Median: median, P90: sorted[max(rank, 1)-1]}
}

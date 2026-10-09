package application

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Grist comes first in the caps (mw-t0z3fu.4): an answer to a tutor is the
// Governor or his child waiting with the app open, and a Builder story can wait
// minutes. While grist is in flight or has just finished, the home host starts
// fewer stories, and while the answers run slow it starts none.
const (
	// DefaultGristRecentSeconds is how long after a grind ended grist still
	// counts as in use, and DefaultGristSlowFactor how many times its par the
	// median of a kind's last answers may reach before the host starts nothing.
	DefaultGristRecentSeconds = 600
	DefaultGristSlowFactor    = 1.5
	gristCapLess              = 2  // the grist cap is the host's cap less this, by default
	GristSlowWindow           = 5  // the last grinds of a kind whose median is judged
	GristParWindow            = 50 // the latest grinds a par is the median of
	gristParMinSamples        = 10 // fewer grinds of a kind have no par of their own
	gristSlowMinSamples       = 3  // fewer than this of a kind's last grinds are not judged
)

// GristRoomLimits are the config file's [dispatch] grist_cap, grist_recent_s
// and grist_slow_factor. A zero value reads its default: the host's cap less 2
// (never under 1), 600 seconds, and 1.5.
type GristRoomLimits struct {
	// Cap is how many sessions a host runs at once while grist is in use.
	Cap int
	// RecentSeconds is how long after the last grind ended grist is still in use.
	RecentSeconds int
	// SlowFactor times a kind's par is the median above which it is slow.
	SlowFactor float64
}

func (l GristRoomLimits) recent() time.Duration {
	if l.RecentSeconds <= 0 {
		return DefaultGristRecentSeconds * time.Second
	}
	return time.Duration(l.RecentSeconds) * time.Second
}

func (l GristRoomLimits) factor() float64 {
	if l.SlowFactor <= 0 {
		return DefaultGristSlowFactor
	}
	return l.SlowFactor
}

// capFor is the sessions a host with hostCap runs at once while grist is in
// use: the grist cap, never above the host's own and never under one.
func (l GristRoomLimits) capFor(hostCap int) int {
	n := l.Cap
	if n <= 0 {
		n = hostCap - gristCapLess
	}
	return max(1, min(n, hostCap))
}

// GristPulses is how a dispatch or mw status asks what grist is doing.
type GristPulses interface {
	Pulse(ctx context.Context, hostCap int) (GristPulse, error)
}

// GristRoom reads the mill's own state on this host: its grind slots, to see a
// grind in flight, and its runs, to see the ones that ended lately and how long
// the answers take. On a host that is not home the mill has run nothing, so
// nothing is active and no cap moves.
type GristRoom struct {
	// Runs is the mill's kept runs; nil reads none.
	Runs GristRunStore
	// Grinding are the mill's grind slots: a held one is a grind in flight.
	Grinding []GristLock
	Limits   GristRoomLimits
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

var _ GristPulses = GristRoom{}

// GristPulse is what grist is doing, as the room check and mw status read it.
type GristPulse struct {
	// Known is whether the mill has a grind in flight or has ever kept a run;
	// a host without one says nothing of grist.
	Known bool
	// InFlight is whether a grind is running now, Recent how many tutor grinds
	// ended within Window, and Active whether either is so.
	InFlight bool
	Recent   int
	Active   bool
	Window   time.Duration
	// HostCap is the cap the host was asked about, and Cap what holds while grist
	// is Active: lower than HostCap, or the same when there is nothing to lower.
	HostCap int
	Cap     int
	// Slow is why the host starts no story: the kind whose last answers took
	// over the factor times their par. Empty when none is slow, and always empty
	// while grist is not Active.
	Slow string
	// Kind, Median and Par are what the status line speaks of: the slowest kind
	// when one is slow, else the kind answered last. Median is 0 when too few of
	// its grinds were kept; Par is 0 when no grist is kept enough to have one.
	Kind   string
	Median float64
	Par    float64
}

// Lowered reports whether grist holds the host to fewer stories than its cap.
func (p GristPulse) Lowered() bool { return p.Active && p.Cap < p.HostCap }

// Line is the mw status line for grist, "" when this host has no mill.
func (p GristPulse) Line() string {
	if !p.Known {
		return ""
	}
	window := fmt.Sprintf("%d min", int(p.Window.Minutes()))
	if p.Window%time.Minute != 0 {
		window = fmt.Sprintf("%d s", int(p.Window.Seconds()))
	}
	state := "quiet (none in " + window + ")"
	switch {
	case p.Active:
		count := fmt.Sprintf("%d in %s", p.Recent, window)
		if p.InFlight {
			count = "grinding now, " + count
		}
		name := "active"
		if p.Slow != "" {
			name = "slow"
		}
		state = fmt.Sprintf("%s (%s)", name, count)
	}
	line := "grist: " + state
	if p.Median > 0 {
		line += fmt.Sprintf(", median %.0f s", p.Median)
		if p.Par > 0 {
			line += fmt.Sprintf(" (par %.0f s)", p.Par)
		}
	}
	return line
}

// CapLine says the cap grist holds the host to, "" when it holds it to none.
func (p GristPulse) CapLine() string {
	if !p.Lowered() {
		return ""
	}
	return fmt.Sprintf("grist first: %d stories at most while it lasts", p.Cap)
}

// Pulse reads what grist is doing for a host that may run hostCap sessions at
// once. A run store that cannot be read is an error, and the caller holds
// nothing back for it.
func (g GristRoom) Pulse(ctx context.Context, hostCap int) (GristPulse, error) {
	now := time.Now
	if g.Now != nil {
		now = g.Now
	}
	pulse := GristPulse{Window: g.Limits.recent(), HostCap: hostCap, Cap: hostCap}
	for _, lock := range g.Grinding {
		if lock == nil {
			continue
		}
		held, err := lock.Held(ctx)
		if err != nil {
			return pulse, fmt.Errorf("asking whether a grind is running: %w", err)
		}
		pulse.InFlight = pulse.InFlight || held
	}
	var timings []GristRunTiming
	if g.Runs != nil {
		all, err := g.Runs.Timings(ctx)
		if err != nil {
			return pulse, fmt.Errorf("reading the mill's runs: %w", err)
		}
		// A forwarded photo is for the Mayor, not a tutor answering.
		for _, t := range all {
			if t.Kind != GristForwardKind {
				timings = append(timings, t)
			}
		}
	}
	sort.SliceStable(timings, func(i, j int) bool { return timings[i].endedAt().Before(timings[j].endedAt()) })
	pulse.Known = pulse.InFlight || len(timings) > 0

	since := now().Add(-pulse.Window)
	var recentKinds []string
	for _, t := range timings {
		if t.endedAt().Before(since) {
			continue
		}
		pulse.Recent++
		pulse.Kind = t.Kind
		recentKinds = append(recentKinds, t.Kind)
	}
	pulse.Active = pulse.InFlight || pulse.Recent > 0
	if !pulse.Active {
		return pulse, nil
	}
	pulse.Cap = g.Limits.capFor(hostCap)

	kinds := map[string]bool{}
	for _, kind := range recentKinds {
		kinds[kind] = true
	}
	names := make([]string, 0, len(kinds))
	for kind := range kinds {
		names = append(names, kind)
	}
	sort.Strings(names)
	worst := 0.0
	for _, kind := range names {
		median, par, ok := kindPace(timings, kind)
		if !ok {
			continue
		}
		if kind == pulse.Kind || pulse.Median == 0 {
			pulse.Median, pulse.Par = median, par
		}
		if par > 0 && median > g.Limits.factor()*par && median/par > worst {
			worst = median / par
			pulse.Kind, pulse.Median, pulse.Par = kind, median, par
			pulse.Slow = fmt.Sprintf("slow grist: %s %.0f s, par %.0f s", kind, median, par)
		}
	}
	return pulse, nil
}

func (t GristRunTiming) endedAt() time.Time {
	if t.Answered.IsZero() {
		return t.Received.Add(time.Duration(t.Seconds * float64(time.Second)))
	}
	return t.Answered
}

// kindPace is the median end to end time of the last GristSlowWindow grinds of
// kind among timings (oldest first), and the par it is judged by: the median of
// the kind's last GristParWindow grinds, or of all grist's when the kind has
// too few of its own. ok is false when too few of the kind were kept to judge;
// par is 0 when no grist is kept enough to have one.
func kindPace(timings []GristRunTiming, kind string) (median, par float64, ok bool) {
	var own, all []float64
	for _, t := range timings {
		seconds := t.EndToEnd()
		all = append(all, seconds)
		if t.Kind == kind {
			own = append(own, seconds)
		}
	}
	if len(own) < gristSlowMinSamples {
		return 0, 0, false
	}
	median = medianOf(own[max(0, len(own)-GristSlowWindow):])
	switch {
	case len(own) >= gristParMinSamples:
		par = medianOf(own[max(0, len(own)-GristParWindow):])
	case len(all) >= gristParMinSamples:
		par = medianOf(all[max(0, len(all)-GristParWindow):])
	}
	return median, par, true
}

func medianOf(seconds []float64) float64 {
	sorted := append([]float64(nil), seconds...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

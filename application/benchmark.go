package application

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// Benchmark is what one close-out measured of the host it ran on and of the
// story it closed (mw-t0z3fu.2): how long the rig's tests took and how busy the
// host was when they started, so that the factory can compare what a host
// usually does with what it really does, and see where more stories at once stop
// landing more work. It is kept in the story's result file in the vault, beside
// what the session reported, and read back from there on every host.
type Benchmark struct {
	Story string
	Rig   string
	Host  string
	// Kind is rig, bug or feature, and model: what a story's par is the median
	// of (KindOf).
	Kind string
	// At is when the close-out recorded it.
	At time.Time

	// GateSeconds is how long the rig's tests ran, waits for the host to calm
	// not counted; LoadAtGate, Cores and MemFreeMB are the host's 1-minute
	// load, core count and available memory when they started.
	GateSeconds float64
	LoadAtGate  float64
	Cores       int
	MemFreeMB   int64
	// RunningCount is how many stories were running on the host at the gate
	// (this one among them), and SwapInPerS and SwapOutPerS are the pages the
	// host was swapping in and out a second then, 0 where it could not tell.
	RunningCount int
	SwapInPerS   float64
	SwapOutPerS  float64

	// Landed says the story was landed. ActualS is the wall time from its
	// dispatch to its landing, and ParS the par it was held to, from the history
	// there was then and ParBasis (ParByKind, ParByRig, ParByAll or ParNone) the
	// part of it the par is the median of.
	Landed   bool
	ActualS  float64
	ParS     float64
	ParBasis string
	// TimeoutUnderLoad says the story was refused for tests that timed out
	// while the host was at or above its core count.
	TimeoutUnderLoad bool
}

// The parts of the history a par is the median of.
const (
	ParByKind = "kind"
	ParByRig  = "rig"
	ParByAll  = "all"
	ParNone   = "none"
)

// The defaults of BenchmarkSettings, which the config file's [benchmark] table
// changes: the last ten gates are a host's usual; a par is the median of the
// last twenty landings of the story's kind, or of its rig, or of all, when
// fewer than five are of the kind; calibration looks at the last fifty
// landings of a kind and flags one whose median par error passes 30%; a story
// that runs past twice its par is trouble.
const (
	DefaultUsualGates        = 10
	DefaultParWindow         = 20
	DefaultParMin            = 5
	DefaultCalibrationWindow = 50
	DefaultErrorFlagPercent  = 30.0
	DefaultOverParFactor     = 2.0
)

// BenchmarkSettings are the thresholds of the benchmark; a zero value reads its
// default, so that nothing is hard-wired where the config file can change it.
type BenchmarkSettings struct {
	// UsualGates is how many of a host's last gates on a rig its usual is the
	// median of.
	UsualGates int
	// ParWindow is how many landings a par is the median of, and ParMin how
	// many of the kind it takes before the kind's own are used.
	ParWindow int
	ParMin    int
	// CalibrationWindow is how many of a kind's last landings its par error is
	// read over, ErrorFlagPercent the median error past which the kind is
	// flagged, and OverParFactor the multiple of par past which a story counts
	// as having run over.
	CalibrationWindow int
	ErrorFlagPercent  float64
	OverParFactor     float64
}

func (s BenchmarkSettings) usualGates() int {
	if s.UsualGates < 1 {
		return DefaultUsualGates
	}
	return s.UsualGates
}

func (s BenchmarkSettings) parWindow() int {
	if s.ParWindow < 1 {
		return DefaultParWindow
	}
	return s.ParWindow
}

func (s BenchmarkSettings) parMin() int {
	if s.ParMin < 1 {
		return DefaultParMin
	}
	return s.ParMin
}

func (s BenchmarkSettings) calibrationWindow() int {
	if s.CalibrationWindow < 1 {
		return DefaultCalibrationWindow
	}
	return s.CalibrationWindow
}

func (s BenchmarkSettings) errorFlagPercent() float64 {
	if s.ErrorFlagPercent <= 0 {
		return DefaultErrorFlagPercent
	}
	return s.ErrorFlagPercent
}

func (s BenchmarkSettings) overParFactor() float64 {
	if s.OverParFactor <= 0 {
		return DefaultOverParFactor
	}
	return s.OverParFactor
}

// BenchmarkBook is where the benchmarks of past close-outs are read from, every
// host's together: the adapter is the story run records in the vault, which
// the hosts' syncs carry. A nil book leaves a close-out without a par and mw
// status without a section.
type BenchmarkBook interface {
	// Recent is every benchmark the vault holds, oldest first.
	Recent(ctx context.Context) ([]Benchmark, error)
}

// KindOf is the kind a story is held to a par of: its rig, whether it is a bug
// or a feature, and the model it was worked on.
func KindOf(rig string, bug bool, model string) string {
	what := "feature"
	if bug {
		what = "bug"
	}
	if model == "" {
		model = "default"
	}
	return rig + "/" + what + "/" + model
}

// landings are the benchmarks of stories that landed in a measurable time,
// the most recent first.
func landings(history []Benchmark) []Benchmark {
	var out []Benchmark
	for _, b := range history {
		if b.Landed && b.ActualS > 0 {
			out = append(out, b)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// median is the middle of values, or the mean of the two in the middle; 0 for
// none.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func actuals(history []Benchmark) []float64 {
	out := make([]float64, len(history))
	for i, b := range history {
		out[i] = b.ActualS
	}
	return out
}

// ParFor is the par of a story of this kind: the median wall time, dispatch to
// landing, of the last ParWindow landings of the kind. With fewer than ParMin
// of the kind it is the same of the rig, and with fewer than ParMin of the rig
// of every landing. It says which of the three it is the median of; a history
// with no landing at all gives no par.
func ParFor(history []Benchmark, rig, kind string, s BenchmarkSettings) (par float64, basis string) {
	recent := landings(history)
	window := s.parWindow()
	last := func(keep func(Benchmark) bool) []Benchmark {
		var out []Benchmark
		for _, b := range recent {
			if keep(b) {
				out = append(out, b)
				if len(out) == window {
					break
				}
			}
		}
		return out
	}
	if of := last(func(b Benchmark) bool { return b.Kind == kind }); len(of) >= s.parMin() {
		return median(actuals(of)), ParByKind
	}
	if of := last(func(b Benchmark) bool { return b.Rig == rig }); len(of) >= s.parMin() {
		return median(actuals(of)), ParByRig
	}
	if of := last(func(Benchmark) bool { return true }); len(of) > 0 {
		return median(actuals(of)), ParByAll
	}
	return 0, ParNone
}

// Ratio is how far over par the work ran: actual over par, 1 at par.
func (b Benchmark) Ratio() float64 {
	if b.ParS <= 0 {
		return 0
	}
	return b.ActualS / b.ParS
}

// GateLine is one host's gate time on one rig, the usual against the latest.
type GateLine struct {
	Rig, Host     string
	Usual, Latest time.Duration
}

// String is the line mw status prints: lampas on laptop: gate usual 5m10s,
// latest 9m02s.
func (g GateLine) String() string {
	return fmt.Sprintf("%s on %s: gate usual %s, latest %s", g.Rig, g.Host, gateClock(g.Usual), gateClock(g.Latest))
}

// gateClock is a gate's length as a person reads it: 5m10s, 9m02s, 1h02m.
func gateClock(d time.Duration) string {
	secs := int(d.Round(time.Second) / time.Second)
	switch {
	case secs >= 3600:
		return fmt.Sprintf("%dh%02dm", secs/3600, secs%3600/60)
	case secs >= 60:
		return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
	}
	return fmt.Sprintf("%ds", secs)
}

// seconds is a number of seconds as a duration.
func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// byTime orders a copy of history oldest first.
func byTime(history []Benchmark) []Benchmark {
	out := slices.Clone(history)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

// GateLines is, for each rig on each host that has recorded a gate, the median
// of its last UsualGates gates against the latest, in rig then host order.
func GateLines(history []Benchmark, s BenchmarkSettings) []GateLine {
	type key struct{ rig, host string }
	gates := map[key][]float64{}
	for _, b := range byTime(history) {
		if b.GateSeconds <= 0 || b.Host == "" {
			continue
		}
		k := key{b.Rig, b.Host}
		gates[k] = append(gates[k], b.GateSeconds)
	}
	var lines []GateLine
	for k, all := range gates {
		recent := all
		if len(recent) > s.usualGates() {
			recent = recent[len(recent)-s.usualGates():]
		}
		lines = append(lines, GateLine{Rig: k.rig, Host: k.host, Usual: seconds(median(recent)), Latest: seconds(all[len(all)-1])})
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Rig != lines[j].Rig {
			return lines[i].Rig < lines[j].Rig
		}
		return lines[i].Host < lines[j].Host
	})
	return lines
}

// Throughput is how much a host landed while it had Running stories at once.
// Little's law gives the rate from the stories alone: Running slots, each busy
// for the time its story took, so Landed stories took the sum of their wall
// times over Running of wall time to land.
type Throughput struct {
	Host    string
	Running int
	Landed  int
	// PerHour is stories landed per hour of wall time at this running count, and
	// ParHoursPerHour the par of what landed, in hours, per hour of wall time:
	// the same measure with each story weighed by how big its kind is.
	PerHour         float64
	ParHoursPerHour float64
	// MedianGate is the median gate of the close-outs at this running count.
	MedianGate time.Duration

	actualS, parS float64
}

// Ratio is how far over par the stories ran, in all, at this running count:
// their wall time over their par; 0 where none had a par.
func (t Throughput) Ratio() float64 {
	if t.parS <= 0 {
		return 0
	}
	return t.actualS / t.parS
}

// String is the line mw status prints: laptop at 4 running: 2.1/h, 2.4 par-h/h,
// gate 6m00s.
func (t Throughput) String() string {
	par := "no par"
	if t.ParHoursPerHour > 0 {
		par = fmt.Sprintf("%.1f par-h/h", t.ParHoursPerHour)
	}
	return fmt.Sprintf("%s at %d running: %.1f/h, %s, gate %s", t.Host, t.Running, t.PerHour, par, gateClock(t.MedianGate))
}

// ThroughputByRunning groups the landings of each host by how many stories were
// running at the gate, in host then running order.
func ThroughputByRunning(history []Benchmark) []Throughput {
	type key struct {
		host    string
		running int
	}
	type tally struct {
		landed         int
		actual, par    float64
		parredActual   float64
		parredLandings int
		gates          []float64
	}
	groups := map[key]*tally{}
	for _, b := range history {
		if b.Host == "" || b.RunningCount < 1 {
			continue
		}
		k := key{b.Host, b.RunningCount}
		if groups[k] == nil {
			groups[k] = &tally{}
		}
		g := groups[k]
		if b.GateSeconds > 0 {
			g.gates = append(g.gates, b.GateSeconds)
		}
		if !b.Landed || b.ActualS <= 0 {
			continue
		}
		g.landed++
		g.actual += b.ActualS
		if b.ParS > 0 {
			g.par += b.ParS
			g.parredActual += b.ActualS
		}
	}
	var out []Throughput
	for k, g := range groups {
		t := Throughput{Host: k.host, Running: k.running, Landed: g.landed, MedianGate: seconds(median(g.gates)), actualS: g.parredActual, parS: g.par}
		if g.actual > 0 {
			t.PerHour = float64(k.running) * float64(g.landed) * 3600 / g.actual
		}
		if g.parredActual > 0 {
			t.ParHoursPerHour = float64(k.running) * g.par / g.parredActual
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Running < out[j].Running
	})
	return out
}

// KindCalibration is how well the par of one kind has matched what its stories
// really took.
type KindCalibration struct {
	Kind string
	// Started is how many stories of the kind, of its last CalibrationWindow,
	// landed or were refused for a timeout under load; Trouble is how many of
	// those were refused for a timeout or ran past OverParFactor times their par.
	// They are the stories the room rule let start that went wrong anyway.
	Started int
	Trouble int
	// Measured is how many of them landed with a par, and MedianErrorPercent the
	// median of |actual - par| / par over them, in percent.
	Measured           int
	MedianErrorPercent float64
	// OverParFactor is the multiple of par Trouble counts a story past.
	OverParFactor float64
	// Flag is the change the kind's par is suggested (try a wider window, or
	// rig-only grouping), empty when its error is within the threshold.
	Flag string
}

// String is the kind's calibration on one line.
func (k KindCalibration) String() string {
	return k.Kind + ": " + k.ErrorLine() + ", " + k.TroubleLine()
}

// ErrorLine is how far off the kind's par has been, and TroubleLine how often
// a story of the kind that the room rule let start went wrong anyway.
func (k KindCalibration) ErrorLine() string {
	return fmt.Sprintf("par off %.0f%% over %d landed", k.MedianErrorPercent, k.Measured)
}

func (k KindCalibration) TroubleLine() string {
	return fmt.Sprintf("%d of %d over %gx par or timed out", k.Trouble, k.Started, k.OverParFactor)
}

// FlagLine is the flag as a line of its own: FLAG <kind>: try a wider window.
func (k KindCalibration) FlagLine() string { return "FLAG " + k.Kind + ": " + k.Flag }

// Calibrate reads each kind's par against what its stories took, over the last
// CalibrationWindow of them, in kind order. A kind whose median error passes the
// threshold is flagged, once it has ParMin measured landings (fewer is no
// evidence the window is wrong), with the change that is suggested: a wider window while
// it has fewer landings than a par is the median of (the par is not steady yet),
// and rig-only grouping once it has (the kind is mixing different work).
func Calibrate(history []Benchmark, s BenchmarkSettings) []KindCalibration {
	byKind := map[string][]Benchmark{}
	for _, b := range byTime(history) {
		if b.Kind == "" || !(b.Landed && b.ActualS > 0 || b.TimeoutUnderLoad) {
			continue
		}
		byKind[b.Kind] = append(byKind[b.Kind], b)
	}
	var out []KindCalibration
	for kind, all := range byKind {
		recent := all
		if len(recent) > s.calibrationWindow() {
			recent = recent[len(recent)-s.calibrationWindow():]
		}
		cal := KindCalibration{Kind: kind, Started: len(recent), OverParFactor: s.overParFactor()}
		var errs []float64
		for _, b := range recent {
			over := b.Landed && b.ParS > 0 && b.ActualS > s.overParFactor()*b.ParS
			if b.TimeoutUnderLoad || over {
				cal.Trouble++
			}
			if b.Landed && b.ParS > 0 {
				errs = append(errs, math.Abs(b.ActualS-b.ParS)/b.ParS*100)
			}
		}
		cal.Measured = len(errs)
		cal.MedianErrorPercent = median(errs)
		if cal.Measured >= s.parMin() && cal.MedianErrorPercent > s.errorFlagPercent() {
			if cal.Measured < s.parWindow() {
				cal.Flag = "try a wider window"
			} else {
				cal.Flag = "try rig-only grouping"
			}
		}
		out = append(out, cal)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// timeoutMarkers are what a rig's tests print when they gave up waiting.
var timeoutMarkers = []string{"test timed out", "waitfor", "timeout"}

// TimeoutUnderLoad says a rig's failed tests failed on a timeout while the host
// was at or above its core count: not the story's fault, and worth a second look
// before the code is blamed.
func TimeoutUnderLoad(output string, at LoadReading) bool {
	if !at.Busy() {
		return false
	}
	lower := strings.ToLower(output)
	for _, marker := range timeoutMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// TimeoutUnderLoadNote is the words a refusal for such a failure carries, so
// that the Mayor reads it as the host's and not the story's.
func TimeoutUnderLoadNote(host string, at LoadReading) string {
	return fmt.Sprintf("timeout under load: the output names a timeout and %s was at load %.1f of %d cores when the tests started", host, at.Load, at.Cores)
}

// The fields a benchmark adds to a story's result file. They sit at the top
// level beside the harness's own, under names the harness does not use.
type benchmarkFields struct {
	Rig              string   `json:"rig,omitempty"`
	Host             string   `json:"host,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	At               string   `json:"benchmark_at,omitempty"`
	GateSeconds      *float64 `json:"gate_seconds"`
	LoadAtGate       float64  `json:"load_at_gate"`
	Cores            int      `json:"cores"`
	MemFreeMB        int64    `json:"mem_free_mb"`
	RunningCount     int      `json:"running_count"`
	SwapInPerS       float64  `json:"swap_in_per_s"`
	SwapOutPerS      float64  `json:"swap_out_per_s"`
	Landed           bool     `json:"landed"`
	ActualS          float64  `json:"actual_s"`
	ParS             float64  `json:"par_s"`
	ParBasis         string   `json:"par_basis,omitempty"`
	TimeoutUnderLoad bool     `json:"timeout_under_load"`
}

// MergeBenchmark adds the benchmark to a result file's JSON, leaving what the
// harness wrote as it was. A file that is not a JSON object is an error.
func MergeBenchmark(printed string, b Benchmark) (string, error) {
	fields := map[string]json.RawMessage{}
	if trimmed := strings.TrimSpace(printed); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
			return "", fmt.Errorf("the session's result is not a JSON object: %w", err)
		}
	}
	gate := b.GateSeconds
	added, err := json.Marshal(benchmarkFields{
		Rig: b.Rig, Host: b.Host, Kind: b.Kind, At: b.At.UTC().Format(time.RFC3339), GateSeconds: &gate,
		LoadAtGate: b.LoadAtGate, Cores: b.Cores, MemFreeMB: b.MemFreeMB, RunningCount: b.RunningCount,
		SwapInPerS: b.SwapInPerS, SwapOutPerS: b.SwapOutPerS, Landed: b.Landed, ActualS: b.ActualS,
		ParS: b.ParS, ParBasis: b.ParBasis, TimeoutUnderLoad: b.TimeoutUnderLoad,
	})
	if err != nil {
		return "", err
	}
	var ours map[string]json.RawMessage
	if err := json.Unmarshal(added, &ours); err != nil {
		return "", err
	}
	for k, v := range ours {
		fields[k] = v
	}
	merged, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	return string(merged) + "\n", nil
}

// ReadBenchmark reads the benchmark out of a story's result file, and whether
// there was one: a result written before close-outs left benchmarks, or by a
// close-out that never ran the gate, has none.
func ReadBenchmark(storyID, printed string) (Benchmark, bool) {
	var f benchmarkFields
	if err := json.Unmarshal([]byte(strings.TrimSpace(printed)), &f); err != nil || f.GateSeconds == nil || f.Host == "" {
		return Benchmark{}, false
	}
	at, _ := time.Parse(time.RFC3339, f.At)
	return Benchmark{
		Story: storyID, Rig: f.Rig, Host: f.Host, Kind: f.Kind, At: at, GateSeconds: *f.GateSeconds,
		LoadAtGate: f.LoadAtGate, Cores: f.Cores, MemFreeMB: f.MemFreeMB, RunningCount: f.RunningCount,
		SwapInPerS: f.SwapInPerS, SwapOutPerS: f.SwapOutPerS, Landed: f.Landed, ActualS: f.ActualS,
		ParS: f.ParS, ParBasis: f.ParBasis, TimeoutUnderLoad: f.TimeoutUnderLoad,
	}, true
}

// BenchmarkReport is the BENCHMARKS section of mw status: what the close-outs
// measured, every host's together.
type BenchmarkReport struct {
	Gates       []GateLine
	Throughput  []Throughput
	Calibration []KindCalibration
}

// ReadBenchmarks is the section from the benchmarks of past close-outs; nil
// when there are none to show.
func ReadBenchmarks(past []Benchmark, s BenchmarkSettings) *BenchmarkReport {
	report := &BenchmarkReport{
		Gates: GateLines(past, s), Throughput: ThroughputByRunning(past), Calibration: Calibrate(past, s),
	}
	if len(report.Gates) == 0 && len(report.Throughput) == 0 && len(report.Calibration) == 0 {
		return nil
	}
	return report
}

// BenchmarksHeading heads the section.
const BenchmarksHeading = "BENCHMARKS"

func (r *BenchmarkReport) write(b *strings.Builder) {
	clip(b, BenchmarksHeading)
	for _, g := range r.Gates {
		clip(b, "  "+g.String())
	}
	for _, t := range r.Throughput {
		clip(b, "  "+t.String())
	}
	for _, k := range r.Calibration {
		clip(b, "  "+k.Kind+": "+k.ErrorLine())
		clip(b, "    "+k.TroubleLine())
		if k.Flag != "" {
			clip(b, "  "+k.FlagLine())
		}
	}
}

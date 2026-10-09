package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// gateRecord is what a close-out measured of the rig's tests and the host they
// ran on (mw-t0z3fu.2). The first gate of a close-out sets the host's reading and
// the stories running; the time of every run is added, so that a gate run twice
// counts both.
type gateRecord struct {
	ran     bool
	seconds float64
	// reading is the host's load, memory and swap when the first gate began, and
	// known says there was one to take.
	reading LoadReading
	known   bool
	running int
	// timeout says the last run failed on a timeout with the host at or above
	// its core count, and at is the reading that said it was.
	timeout bool
	at      LoadReading
}

// beginGate takes the host's reading and the count of stories running at the
// start of the first gate of a close-out.
func (n Next) beginGate(ctx context.Context, c *closeOut, reading LoadReading) {
	if c.gate.ran {
		return
	}
	c.gate.ran = true
	if n.Load != nil && (reading.Cores > 0 || reading.Load > 0) {
		c.gate.reading, c.gate.known = reading, true
	}
	if running, err := n.Tracker.RunningStories(ctx, n.Host); err == nil {
		for _, d := range running {
			if !d.Hitl() {
				c.gate.running++
			}
		}
	}
}

// timedChecks runs the rig's tests and adds how long they took to the gate.
func (n Next) timedChecks(ctx context.Context, c *closeOut, dir string) (Checked, error) {
	started := n.now()
	checked, err := n.Checks.Run(ctx, c.path.Rig, dir)
	c.gate.seconds += n.now().Sub(started).Seconds()
	return checked, err
}

// settle says what the last run of the gate came to, with the reading it was
// judged against: tests that failed, naming a timeout, with the host busy.
func (g *gateRecord) settle(checked Checked, err error, reading LoadReading) {
	g.timeout, g.at = false, reading
	if err == nil && !checked.NotRun && !checked.Passed && TimeoutUnderLoad(checked.Output, reading) {
		g.timeout = true
	}
}

// recordBenchmark writes the close-out's benchmark into the story's result file
// and, when it has a book of past ones, says in the report what it came to. It
// never fails the close-out: a result that cannot be read or written is a note.
func (n Next) recordBenchmark(ctx context.Context, c *closeOut, report *NextReport, landed bool) {
	if !c.gate.ran {
		return
	}
	name := ResultFileNameForAttempt(c.attempt())
	printed, err := n.Vault.ReadRunFile(ctx, c.id, name)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the benchmark of %s could not be recorded: %v", c.id, err))
		return
	}
	bug := domain.IsBugStory(c.detail.Type, c.detail.Story.Title) || slices.Contains(c.detail.Labels, "bug")
	b := Benchmark{
		Story: c.id, Rig: c.path.Rig, Host: n.Host, Kind: KindOf(c.path.Rig, bug, string(c.path.Model)),
		At: n.now().UTC(), GateSeconds: c.gate.seconds, RunningCount: c.gate.running, Landed: landed,
		TimeoutUnderLoad: !landed && c.gate.timeout,
	}
	if c.gate.known {
		r := c.gate.reading
		b.LoadAtGate, b.Cores, b.MemFreeMB = r.Load, r.Cores, r.MemAvailableMB
		if r.SwapKnown {
			b.SwapInPerS, b.SwapOutPerS = r.SwapInKBPerS, r.SwapOutKBPerS
		}
	}

	var history []Benchmark
	if n.Benchmarks != nil {
		all, err := n.Benchmarks.Recent(ctx)
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the benchmarks of past close-outs could not be read, so %s has no par: %v", c.id, err))
		}
		// This story's own earlier record (a close-out run again) is not its par.
		history = slices.DeleteFunc(all, func(past Benchmark) bool { return past.Story == c.id })
	}
	if landed {
		if started := c.detail.ClaimStarted(); !started.IsZero() && b.At.After(started) {
			b.ActualS = b.At.Sub(started).Seconds()
		}
		if b.ActualS > 0 {
			b.ParS, b.ParBasis = ParFor(history, c.path.Rig, b.Kind, n.Bench)
		}
	}

	merged, err := MergeBenchmark(printed, b)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the benchmark of %s could not be recorded: %v", c.id, err))
		return
	}
	if _, err := n.Vault.PutRunFile(ctx, c.id, name, merged); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the benchmark of %s could not be recorded: %v", c.id, err))
		return
	}
	if n.Benchmarks != nil {
		report.Bench = benchmarkSummary(b, append(history, b), n.Bench)
	}
}

// benchmarkSummary is the lines the report prints of a benchmark: the gate, the
// host as it began, the story against its par, and the calibration of its kind
// beside every kind that is flagged.
func benchmarkSummary(b Benchmark, history []Benchmark, s BenchmarkSettings) []string {
	line := fmt.Sprintf("gate %s", gateClock(seconds(b.GateSeconds)))
	if b.Cores > 0 {
		line += fmt.Sprintf(" at load %.1f of %d cores", b.LoadAtGate, b.Cores)
	}
	if b.MemFreeMB > 0 {
		line += fmt.Sprintf(", %d MB free", b.MemFreeMB)
	}
	if b.RunningCount > 0 {
		line += fmt.Sprintf(", %d running", b.RunningCount)
	}
	lines := []string{line}
	if b.Landed && b.ParS > 0 {
		lines = append(lines, fmt.Sprintf("%s took %s against a par of %s by %s: %.1fx par",
			b.Kind, clockOf(b.ActualS), clockOf(b.ParS), b.ParBasis, b.Ratio()))
	}
	for _, cal := range Calibrate(history, s) {
		switch {
		case cal.Flag != "":
			lines = append(lines, cal.FlagLine()+" ("+cal.ErrorLine()+")")
		case cal.Kind == b.Kind:
			lines = append(lines, cal.String())
		}
	}
	return lines
}

func clockOf(s float64) string { return gateClock(time.Duration(s * float64(time.Second))) }

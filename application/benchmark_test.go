package application_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// landing is one landed story of a fixed fake history: when it landed (minutes
// after the start of the history), how long it ran and the par it was given.
func landing(host, rig, kind string, minute int, running int, actual, par float64) application.Benchmark {
	return application.Benchmark{
		Story: "mw-h." + kind + "." + time.Duration(minute).String(), Host: host, Rig: rig, Kind: kind,
		At:     time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute),
		Landed: true, RunningCount: running, ActualS: actual, ParS: par, GateSeconds: 300,
	}
}

func TestParIsTheMedianOfTheLastLandingsOfTheSameKind(t *testing.T) {
	var history []application.Benchmark
	// Seven landings of one kind; with a window of five only the last five count.
	for i, actual := range []float64{9000, 9000, 100, 200, 300, 400, 500} {
		history = append(history, landing("laptop", "lampas", "lampas/feature/sonnet", i, 3, actual, 0))
	}
	history = append(history, landing("laptop", "lampas", "lampas/bug/sonnet", 20, 3, 1, 0))

	par, basis := application.ParFor(history, "lampas", "lampas/feature/sonnet", application.BenchmarkSettings{ParWindow: 5, ParMin: 5})
	if par != 300 || basis != application.ParByKind {
		t.Errorf("expected par 300 by kind from the last five, got %v by %q", par, basis)
	}
}

func TestParFallsBackToTheRigThenToAllWhenTheKindHasTooFewLandings(t *testing.T) {
	settings := application.BenchmarkSettings{ParWindow: 20, ParMin: 5}
	var history []application.Benchmark
	// Four of the kind (under the five needed), eight more of the rig.
	for i := 0; i < 4; i++ {
		history = append(history, landing("laptop", "lampas", "lampas/bug/opus", i, 3, 1000, 0))
	}
	for i := 0; i < 8; i++ {
		history = append(history, landing("laptop", "lampas", "lampas/feature/sonnet", 10+i, 3, 600, 0))
	}
	// And a rig that has only two landings of its own.
	history = append(history, landing("laptop", "gristle", "gristle/feature/sonnet", 30, 3, 50, 0),
		landing("laptop", "gristle", "gristle/feature/sonnet", 31, 3, 70, 0))

	par, basis := application.ParFor(history, "lampas", "lampas/bug/opus", settings)
	// The rig's twelve landings: eight of 600 and four of 1000.
	if par != 600 || basis != application.ParByRig {
		t.Errorf("expected the rig's median 600, got %v by %q", par, basis)
	}

	par, basis = application.ParFor(history, "gristle", "gristle/feature/sonnet", settings)
	// Two of the rig is too few too, so every landing counts: 14 of them.
	if basis != application.ParByAll || par != 600 {
		t.Errorf("expected the median of all 14 landings (600), got %v by %q", par, basis)
	}

	if par, basis = application.ParFor(nil, "lampas", "lampas/feature/sonnet", settings); par != 0 || basis != application.ParNone {
		t.Errorf("expected no par from no history, got %v by %q", par, basis)
	}
}

func TestParIgnoresARefusalAndAStoryWithNoTime(t *testing.T) {
	history := []application.Benchmark{
		landing("laptop", "lampas", "k", 1, 1, 100, 0),
		{Story: "refused", Host: "laptop", Rig: "lampas", Kind: "k", ActualS: 9999, Landed: false, At: time.Unix(10, 0)},
		landing("laptop", "lampas", "k", 2, 1, 0, 0),
	}
	par, _ := application.ParFor(history, "lampas", "k", application.BenchmarkSettings{ParMin: 1})
	if par != 100 {
		t.Errorf("expected par 100 from the one real landing, got %v", par)
	}
}

func TestThroughputIsLandedAndParHoursPerWallHourAtEachRunningCount(t *testing.T) {
	var history []application.Benchmark
	// At 4 running: three stories that each took 2h against a par of 1h.
	for i := 0; i < 3; i++ {
		history = append(history, landing("laptop", "lampas", "k", i, 4, 7200, 3600))
	}
	// At 2 running: two stories that each took 1h against a par of 1h.
	for i := 0; i < 2; i++ {
		history = append(history, landing("laptop", "lampas", "k", 10+i, 2, 3600, 3600))
	}
	history = append(history, landing("desktop", "lampas", "k", 20, 1, 1800, 1800))

	got := application.ThroughputByRunning(history)
	if len(got) != 3 {
		t.Fatalf("expected a row for desktop at 1, laptop at 2 and laptop at 4, got %+v", got)
	}
	if got[0].Host != "desktop" || got[1].Host != "laptop" || got[1].Running != 2 || got[2].Running != 4 {
		t.Errorf("expected rows ordered by host then running count, got %+v", got)
	}
	// Four slots busy for 3 x 2h of story is 1.5h of wall time: 3 landed in it.
	// k * N / (sum of actual in hours) = 4 * 3 / 6 = 2 an hour.
	if four := got[2]; four.Landed != 3 || four.PerHour != 2 || four.ParHoursPerHour != 2 {
		t.Errorf("expected 3 landed at 2/h and 2 par-h/h at 4 running, got %+v", four)
	}
	// At 2 running: 2 * 2 / 2 = 2 an hour at par.
	if two := got[1]; two.PerHour != 2 || two.ParHoursPerHour != 2 {
		t.Errorf("expected 2/h at 2 running, got %+v", two)
	}
	// Over par shows: the 4-running row ran at twice its par, the other did not.
	if got[2].Ratio() != 2 || got[1].Ratio() != 1 {
		t.Errorf("expected ratios 2 and 1 of actual to par, got %v and %v", got[2].Ratio(), got[1].Ratio())
	}
}

func TestTheGateLineSaysUsualAgainstLatest(t *testing.T) {
	var history []application.Benchmark
	// Eleven gates on the laptop for lampas: the first (oldest) is outside the
	// last ten. Ten of them from 310s, then the latest at 542s.
	old := landing("laptop", "lampas", "k", 0, 1, 1, 1)
	old.GateSeconds = 4000
	history = append(history, old)
	for i := 1; i <= 9; i++ {
		g := landing("laptop", "lampas", "k", i, 1, 1, 1)
		g.GateSeconds = 310
		history = append(history, g)
	}
	latest := landing("laptop", "lampas", "k", 10, 1, 1, 1)
	latest.GateSeconds = 542
	history = append(history, latest)
	// Another host's gates are its own.
	other := landing("desktop", "lampas", "k", 11, 1, 1, 1)
	other.GateSeconds = 61
	history = append(history, other)

	lines := application.GateLines(history, application.BenchmarkSettings{UsualGates: 10})
	if len(lines) != 2 {
		t.Fatalf("expected a line each for laptop and desktop, got %+v", lines)
	}
	var said []string
	for _, l := range lines {
		said = append(said, l.String())
	}
	joined := strings.Join(said, "\n")
	for _, want := range []string{"lampas on laptop: gate usual 5m10s, latest 9m02s", "lampas on desktop: gate usual 1m01s, latest 1m01s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q, got:\n%s", want, joined)
		}
	}
}

func TestCalibrationReportsEachKindsParErrorAndFlagsOneThatIsOff(t *testing.T) {
	settings := application.BenchmarkSettings{CalibrationWindow: 50, ErrorFlagPercent: 30, OverParFactor: 2, ParWindow: 20}
	var history []application.Benchmark
	// A steady kind: par 100, actuals 90, 110, 100, 120: errors 10, 10, 0, 20%.
	for i, actual := range []float64{90, 110, 100, 120} {
		history = append(history, landing("laptop", "lampas", "lampas/feature/sonnet", i, 3, actual, 100))
	}
	// An unsteady kind: par 100, actuals 200, 50, 400, 100, 100 (errors 100, 50,
	// 300, 0, 0%: median 50%), one over 2x par, and a refusal for a timeout.
	for i, actual := range []float64{200, 50, 400, 100, 100} {
		history = append(history, landing("laptop", "lampas", "lampas/bug/opus", 10+i, 3, actual, 100))
	}
	refused := landing("laptop", "lampas", "lampas/bug/opus", 20, 3, 0, 0)
	refused.Landed, refused.TimeoutUnderLoad = false, true
	history = append(history, refused)

	got := application.Calibrate(history, settings)
	if len(got) != 2 {
		t.Fatalf("expected two kinds, got %+v", got)
	}
	// Kinds come back in name order: bug before feature.
	unsteady, steady := got[0], got[1]
	if steady.Kind != "lampas/feature/sonnet" || steady.MedianErrorPercent != 10 || steady.Flag != "" {
		t.Errorf("expected the feature kind steady at 10%% with no flag, got %+v", steady)
	}
	if unsteady.Kind != "lampas/bug/opus" || unsteady.MedianErrorPercent != 50 || unsteady.Flag == "" {
		t.Errorf("expected the bug kind off by 50%% and flagged, got %+v", unsteady)
	}
	// Five landings is par_min but under the par window, so the suggestion is a wider window.
	if !strings.Contains(unsteady.Flag, "wider window") {
		t.Errorf("expected the flag to suggest a wider window, got %q", unsteady.Flag)
	}
	// 6 started: the one at 400 ran over 2x par (200 is not over: 2x is not past 2x)
	// and the one refused for a timeout.
	if unsteady.Started != 6 || unsteady.Trouble != 2 {
		t.Errorf("expected 2 of 6 over 2x par or timed out, got %+v", unsteady)
	}
	if !strings.Contains(unsteady.String(), "50%") || !strings.Contains(unsteady.String(), "2 of 6") {
		t.Errorf("expected the line to say 50%% and 2 of 6, got %q", unsteady.String())
	}
}

func TestAKindWithFewerLandingsThanParMinIsNeverFlaggedButStillReportsItsError(t *testing.T) {
	settings := application.BenchmarkSettings{ErrorFlagPercent: 30, ParMin: 5}
	for landings := 1; landings < 5; landings++ {
		var history []application.Benchmark
		for i := 0; i < landings; i++ {
			history = append(history, landing("desktop", "trade-tracker", "trade-tracker/feature/sonnet", i, 1, 156, 100))
		}
		got := application.Calibrate(history, settings)
		if len(got) != 1 || got[0].Flag != "" {
			t.Errorf("%d landings: expected no flag under par_min, got %+v", landings, got)
		}
		if want := fmt.Sprintf("par off 56%% over %d landed", landings); got[0].ErrorLine() != want {
			t.Errorf("%d landings: expected the line %q, got %q", landings, want, got[0].ErrorLine())
		}
	}
}

func TestAKindWithParMinLandingsOffIsFlaggedToTryAWiderWindow(t *testing.T) {
	settings := application.BenchmarkSettings{ErrorFlagPercent: 30, ParMin: 5}
	var history []application.Benchmark
	for i := 0; i < 5; i++ {
		history = append(history, landing("desktop", "trade-tracker", "trade-tracker/feature/sonnet", i, 1, 156, 100))
	}
	got := application.Calibrate(history, settings)
	if len(got) != 1 || !strings.Contains(got[0].Flag, "wider window") {
		t.Errorf("expected a flag suggesting a wider window at par_min landings, got %+v", got)
	}
}

func TestACalibrationFlagWithManyLandingsSuggestsFallingBackToTheRig(t *testing.T) {
	settings := application.BenchmarkSettings{ParWindow: 4, ErrorFlagPercent: 30}
	var history []application.Benchmark
	for i := 0; i < 6; i++ {
		history = append(history, landing("laptop", "lampas", "k", i, 3, 300, 100))
	}
	got := application.Calibrate(history, settings)
	if len(got) != 1 || !strings.Contains(got[0].Flag, "rig-only") {
		t.Errorf("expected a flag suggesting rig-only grouping, got %+v", got)
	}
}

func TestATimeoutIsOnlyUnderLoadWhenTheHostWasBusy(t *testing.T) {
	busy := application.LoadReading{Load: 39, Cores: 20}
	calm := application.LoadReading{Load: 3, Cores: 20}
	cases := []struct {
		output  string
		reading application.LoadReading
		want    bool
	}{
		{"--- FAIL: TestX\n    Test timed out after 30s\n", busy, true},
		{"waitFor: condition never held", busy, true},
		{"panic: test Timeout exceeded", busy, true},
		{"Test timed out after 30s", calm, false},
		{"--- FAIL: TestX\n    got 3, want 4\n", busy, false},
		{"", busy, false},
	}
	for _, c := range cases {
		if got := application.TimeoutUnderLoad(c.output, c.reading); got != c.want {
			t.Errorf("TimeoutUnderLoad(%q, load %v) = %v, want %v", c.output, c.reading.Load, got, c.want)
		}
	}
}

func TestTheBenchmarkIsMergedIntoTheResultAndReadBack(t *testing.T) {
	result := `{"subtype":"success","num_turns":7,"session_id":"s1"}`
	b := application.Benchmark{
		Story: "mw-x.1", Rig: "lampas", Host: "laptop", Kind: "lampas/feature/sonnet",
		At: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), GateSeconds: 310, LoadAtGate: 3.5, Cores: 20, MemFreeMB: 9000,
		RunningCount: 4, SwapInPerS: 1.5, SwapOutPerS: 2.5, Landed: true, ParS: 600, ActualS: 900, ParBasis: application.ParByKind,
	}
	merged, err := application.MergeBenchmark(result, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"gate_seconds":310`, `"load_at_gate":3.5`, `"cores":20`, `"mem_free_mb":9000`,
		`"running_count":4`, `"swap_in_per_s":1.5`, `"swap_out_per_s":2.5`, `"par_s":600`, `"actual_s":900`, `"kind":"lampas/feature/sonnet"`,
		`"subtype":"success"`, `"session_id":"s1"`} {
		if !strings.Contains(merged, want) {
			t.Errorf("expected the result to carry %s, got %s", want, merged)
		}
	}
	// What the harness wrote still reads as a result.
	if r, err := application.ReadSessionResult(merged); err != nil || r.SessionID != "s1" || r.Turns != 7 {
		t.Errorf("expected the merged file to read as the session's result, got %+v, %v", r, err)
	}
	back, ok := application.ReadBenchmark("mw-x.1", merged)
	if !ok || back.GateSeconds != 310 || back.RunningCount != 4 || back.ParS != 600 || !back.Landed || back.Host != "laptop" || !back.At.Equal(b.At) {
		t.Errorf("expected the benchmark read back whole, got %+v (%v)", back, ok)
	}
	if _, ok := application.ReadBenchmark("mw-x.2", result); ok {
		t.Errorf("a result with no benchmark in it is not one")
	}
}

func TestTheSlotWaitIsTwiceTheRigsSlowerGateOnTheHostAndNeverUnderTheFloor(t *testing.T) {
	floor := 20 * time.Minute
	gate := func(rig, host string, minutes ...float64) []application.Benchmark {
		var out []application.Benchmark
		for _, m := range minutes {
			out = append(out, application.Benchmark{Rig: rig, Host: host, GateSeconds: m * 60})
		}
		return out
	}
	cases := []struct {
		name    string
		history []application.Benchmark
		want    time.Duration
	}{
		{"no history", nil, floor},
		{"a quick rig keeps the floor", gate("lampas", "laptop", 3, 4), floor},
		{"usual 29m", gate("lampas", "laptop", 29, 29, 29), 58 * time.Minute},
		{"a latest slower than the usual counts", gate("lampas", "laptop", 10, 10, 10, 30), 60 * time.Minute},
		{"another host's gates do not count", gate("lampas", "vps", 40), floor},
		{"another rig's gates do not count", gate("millwright", "laptop", 40), floor},
	}
	for _, c := range cases {
		if got := application.SlotWaitFor(c.history, "lampas", "laptop", application.BenchmarkSettings{}, floor); got != c.want {
			t.Errorf("%s: want %s, got %s", c.name, c.want, got)
		}
	}
}

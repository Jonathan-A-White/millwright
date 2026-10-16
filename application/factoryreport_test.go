package application

import (
	"testing"
	"time"
)

func TestParseReportSinceReadsDaysADayAndATime(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for text, want := range map[string]string{
		"":                     "2026-10-04",
		"7d":                   "2026-10-04",
		"1d":                   "2026-10-10",
		"2026-10-09":           "2026-10-09",
		"2026-10-09T20:00:00Z": "2026-10-09",
	} {
		got, err := ParseReportSince(text, now)
		if err != nil || got.Format(LedgerDate) != want {
			t.Errorf("--since %q: got %v, %v; want %s", text, got, err, want)
		}
	}
	for _, bad := range []string{"soon", "0d", "-3d", "7w"} {
		if _, err := ParseReportSince(bad, now); err == nil {
			t.Errorf("--since %q was accepted", bad)
		}
	}
}

func TestParseLedgerLineReadsTheColumnsAReportNeeds(t *testing.T) {
	line := "| 2026-10-08 | The story (mw-x.1) | not landed (tests-fail): the gate was red | sonnet/high | — | session abc |"
	entry, ok := ParseLedgerLine(line)
	if !ok {
		t.Fatalf("the line was not read: %s", line)
	}
	if entry.StoryID != "mw-x.1" || entry.Model != "sonnet" || entry.Effort != "high" || entry.Landed() {
		t.Errorf("read %+v", entry)
	}
	if reason, refused := entry.Refusal(); !refused || reason != "tests-fail" {
		t.Errorf("refusal read as %q, %v", reason, refused)
	}

	old, _ := ParseLedgerLine("| 2026-10-08 | mw-x.2 | not landed: no commits | unknown | — | — |")
	if reason, refused := old.Refusal(); !refused || reason != LedgerReasonUnspecified || old.StoryID != "mw-x.2" || old.Model != "" {
		t.Errorf("an old refusal read as %+v", old)
	}
	for _, not := range []string{"", "# Builder — ledger", "|-|-|-|-|", "| yesterday | x | landed | a | b |"} {
		if _, ok := ParseLedgerLine(not); ok {
			t.Errorf("%q was read as a ledger row", not)
		}
	}
}

func TestNewTrendReadsAFivePercentBandAndTheSenseOfTheMeasure(t *testing.T) {
	for _, c := range []struct {
		now, before    float64
		lowerIsBetter  bool
		arrow, verdict string
	}{
		{10, 20, true, ArrowDown, VerdictBetter},
		{12, 12, false, ArrowLevel, ""},
		{104, 100, true, ArrowLevel, ""},
		{105, 100, true, ArrowLevel, ""},
		{106, 100, true, ArrowUp, VerdictWorse},
		{106, 100, false, ArrowUp, VerdictBetter},
		{94, 100, false, ArrowDown, VerdictWorse},
		{0, 0, true, ArrowLevel, ""},
		{3, 0, true, ArrowUp, VerdictWorse},
	} {
		got := NewTrend(c.now, c.before, c.lowerIsBetter)
		if got.Arrow != c.arrow || got.Verdict != c.verdict || got.Previous != c.before {
			t.Errorf("%v after %v (lower is better %v): got %+v, want %s %s", c.now, c.before, c.lowerIsBetter, got, c.arrow, c.verdict)
		}
	}
	if text := NewTrend(10, 20, true).Text(); text != " ↓ better" {
		t.Errorf("the text of a falling refusal rate is %q", text)
	}
	if text := (Trend{}).Text(); text != "" {
		t.Errorf("a number with nothing to compare with has the text %q", text)
	}
}

func TestPercentileIsTheNearestRank(t *testing.T) {
	ratios := []float64{2.5, 0.8, 1.2, 1.0}
	if got := percentile(ratios, 0.9); got != 2.5 {
		t.Errorf("p90 of four ratios is %v, want 2.5", got)
	}
	if got := percentile(nil, 0.9); got != 0 {
		t.Errorf("p90 of none is %v", got)
	}
	if got := twoPlaces(median(ratios)); got != 1.1 {
		t.Errorf("median is %v, want 1.1", got)
	}
}

func TestEveryRatioOfTheReportIsZeroOverNothing(t *testing.T) {
	if got := percentOf(1, 3); got != 33.3 {
		t.Errorf("1 of 3 is %v%%, want 33.3", got)
	}
	if got := percentOf(2, 0); got != 0 {
		t.Errorf("2 of nothing is %v%%, want 0", got)
	}
	if got := fraction(3, 0); got != 0 {
		t.Errorf("3 over nothing is %v, want 0", got)
	}
	if q := sumQuality(nil); q.RefusalRate != 0 || q.AttemptsPerLanding != 0 {
		t.Errorf("an empty window has %+v", q)
	}
}

// TestHeavyStoriesCountOnlyOwnAttempts is mw-gq6.486: a story started three
// times whose other attempts mw cause named test, tooling or process is not
// heavy, and one with three attempts of its own is, at that count.
func TestHeavyStoriesCountOnlyOwnAttempts(t *testing.T) {
	entries := []reportEntry{
		{id: "mw-a", attempts: 3, own: 1},
		{id: "mw-b", attempts: 5, own: 3},
		{id: "mw-c", attempts: 1, own: 1},
		{id: "mw-d", attempts: 3, own: 2, exhausted: true},
	}
	got := FactoryReport{}.heavyStories(entries)
	want := []HeavyStory{{ID: "mw-b", Attempts: 3}, {ID: "mw-d", Attempts: 3}}
	if len(got) != len(want) {
		t.Fatalf("heavy stories %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("heavy story %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

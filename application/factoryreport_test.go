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

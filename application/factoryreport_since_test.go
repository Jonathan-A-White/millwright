package application

import (
	"testing"
	"time"
)

func TestReadReportSinceTellsAWindowFromALanding(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	for text, want := range map[string]string{
		"7d":         "since 2026-10-04",
		"2026-10-09": "since 2026-10-09",
		"mw-gq6.425": "around mw-gq6.425",
		"9e7d278":    "around 9e7d278",
		"9e7d278c8a1f0b2e3d4c5b6a7980f1e2d3c4b5a6": "around 9e7d278c8a1f0b2e3d4c5b6a7980f1e2d3c4b5a6",
	} {
		since, around, err := ReadReportSince(text, now)
		got := "since " + since.Format(LedgerDate)
		if around != "" {
			got = "around " + around
		}
		if err != nil || got != want {
			t.Errorf("--since %q: got %q, %v; want %q", text, got, err, want)
		}
	}
	for _, text := range []string{"yesterday", "7 days", "zz", "re-runs", "tester-mw"} {
		if _, _, err := ReadReportSince(text, now); err == nil {
			t.Errorf("--since %q was read", text)
		}
	}
}

func TestSubjectStoryIsTheStoryAMillwrightCommitWasMadeFor(t *testing.T) {
	for subject, want := range map[string]string{
		"Merge branch 'mw/mw-gq6.435' into HEAD":                       "mw-gq6.435",
		"The answer line quotes a named channel for sh (mw-gq6.435)":   "mw-gq6.435",
		"Version 0.1.86 (mw-2zh33g.9)":                                 "mw-2zh33g.9",
		"Merge branch 'main' into mw/mw-x.1":                           "",
		"Rework the re-runs (fix-up)":                                  "",
		"Tidy the docs":                                                "",
		"Merge remote-tracking branch 'origin/main' (and more) into x": "",
	} {
		if got := SubjectStory(subject); got != want {
			t.Errorf("SubjectStory(%q) = %q, want %q", subject, got, want)
		}
	}
}

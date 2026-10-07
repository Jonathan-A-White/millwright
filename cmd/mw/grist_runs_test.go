package main

import (
	"testing"
	"time"
)

func TestRunsSinceIsADurationADateOrATime(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for text, want := range map[string]time.Time{
		"":                     {},
		"24h":                  now.Add(-24 * time.Hour),
		"2026-10-01":           time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		"2026-10-07T09:30:00Z": time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC),
	} {
		got, err := parseRunsSince(text, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("--since %q: expected %v, got %v %v", text, want, got, err)
		}
	}
	if _, err := parseRunsSince("last tuesday", now); err == nil {
		t.Error("expected a refusal of a --since that is none of the three")
	}
}

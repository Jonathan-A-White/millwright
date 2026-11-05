package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
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

// mw-gq6.692: mw grist tidy deletes the attachments of a run 30 days old and
// says so, and leaves a younger one alone.
func TestGristTidyDeletesAThirtyDayOldRunsMedia(t *testing.T) {
	state, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.GristStateDirEnv, state)
	for name, age := range map[string]time.Duration{"direct:old": 31 * 24 * time.Hour, "direct:new": 29 * 24 * time.Hour} {
		dir := filepath.Join(state, "runs", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		timing := fmt.Sprintf(`{"txid":%q,"kind":"reading","received":%q}`, name, time.Now().Add(-age).UTC().Format(time.RFC3339))
		for file, body := range map[string]string{"timing.json": timing, "attachment-1.webm": "audio"} {
			if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := newGristTidyCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "runs", "direct:old", "attachment-1.webm")); !os.IsNotExist(err) {
		t.Fatalf("expected the old run's media gone, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "runs", "direct:old", "timing.json")); err != nil {
		t.Fatalf("expected the old run's record kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "runs", "direct:new", "attachment-1.webm")); err != nil {
		t.Fatalf("expected the younger run's media kept: %v", err)
	}
	if !strings.Contains(out.String(), "direct:old") || strings.Contains(out.String(), "direct:new") {
		t.Fatalf("expected the report to name the old run alone, got:\n%s", out.String())
	}
}

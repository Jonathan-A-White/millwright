package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mw grist stats --kind prints the table from the timing files kept under the
// state directory.
func TestGristStatsPrintsTheTableFromKeptTimingFiles(t *testing.T) {
	state := t.TempDir()
	t.Setenv("MW_GRIST_STATE_DIR", state)
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	for i, harness := range []float64{10, 20, 30} {
		dir := filepath.Join(state, "runs", "direct:run"+string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		received := at.Add(time.Duration(i) * time.Minute)
		timing, _ := json.Marshal(map[string]any{
			"txid": "direct:run" + string(rune('a'+i)), "kind": "tutor-turn", "received": received,
			"sent": received.Add(-2 * time.Second), "harness_seconds": harness, "seconds": harness + 2,
		})
		if err := os.WriteFile(filepath.Join(dir, "timing.json"), timing, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runMw(t, "grist", "stats", "--kind", "tutor-turn", "--last", "20")
	if err != nil {
		t.Fatalf("mw grist stats failed: %v\n%s", err, out)
	}
	for _, want := range []string{"queue", "harness", "total", "20.0s", "30.0s"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if _, err := runMw(t, "grist", "stats"); err == nil {
		t.Error("expected mw grist stats to want --kind")
	}
}

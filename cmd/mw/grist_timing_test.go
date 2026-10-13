package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// mw grist timing --help says what each phase means.
func TestGristTimingHelpSaysWhatEachPhaseMeans(t *testing.T) {
	out, err := runMw(t, "grist", "timing", "--help")
	if err != nil {
		t.Fatalf("mw grist timing --help failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"--otlp", "--out", "OTLP/JSON", "wake", "pickup", "slot", "startup", "model", "deliver", "total", "seconds", "tap_to_sent", "delivered_to_seen", "seen_to_shown", "round_trip",
		"noticed", "grind slots", "harness", "--since", "--app", "--kind",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the help:\n%s", want, out)
		}
	}
}

// mw grist timing reads the real record, an old line beside a new one.
func TestGristTimingReadsTheRecordGrindsJSONLHolds(t *testing.T) {
	state := t.TempDir()
	t.Setenv("MW_GRIST_STATE_DIR", state)
	record := grist.New(state)
	wake, total := 3.0, 8.4
	at := time.Now().Add(-time.Hour)
	for _, line := range []application.GrindLine{
		{Time: at, Txid: "direct:old", App: "lampas", Kind: "verse-read", Status: "answered", Seconds: 7.5},
		{Time: at, Txid: "direct:new", App: "lampas", Kind: "verse-read", Status: "answered", Seconds: 5.2, WakeSeconds: &wake, TotalSeconds: &total},
		{Time: at, Txid: "direct:cairn", App: "cairn", Kind: "sweep", Status: "answered", Seconds: 1},
	} {
		if err := record.Append(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runMw(t, "grist", "timing", "--app", "lampas", "--kind", "verse-read", "--since", "24h")
	if err != nil {
		t.Fatalf("mw grist timing failed: %v\n%s", err, out)
	}
	for _, want := range []string{"lampas verse-read: 2 grinds", "seconds", "wake", "total", "8.4s"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "cairn") {
		t.Errorf("cairn's grinds were counted:\n%s", out)
	}
}

// mw grist timing --otlp writes the real record as OTLP JSON to stdout, or to
// the file --out names, and --out alone is refused.
func TestGristTimingOtlpWritesTheRecordAsTraces(t *testing.T) {
	state := t.TempDir()
	t.Setenv("MW_GRIST_STATE_DIR", state)
	wake, total := 3.0, 8.4
	line := application.GrindLine{
		Time: time.Now().Add(-time.Hour), Txid: "direct:cacaf9063f4a4e1fa3b8e2b5f0f1b8b1849a",
		App: "lampas", Kind: "verse-read", Status: "answered", Seconds: 5.2, WakeSeconds: &wake, TotalSeconds: &total,
	}
	if err := grist.New(state).Append(context.Background(), line); err != nil {
		t.Fatal(err)
	}
	check := func(body string) {
		t.Helper()
		var doc application.OtlpDocument
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatalf("not OTLP JSON: %v\n%s", err, body)
		}
		if len(doc.ResourceSpans) != 1 || len(doc.ResourceSpans[0].ScopeSpans[0].Spans) != 2 {
			t.Fatalf("expected the root and the wake span:\n%s", body)
		}
		if got := doc.ResourceSpans[0].ScopeSpans[0].Spans[0].TraceID; got != "cacaf9063f4a4e1fa3b8e2b5f0f1b8b1" {
			t.Errorf("trace id %s", got)
		}
	}
	out, err := runMw(t, "grist", "timing", "--otlp")
	if err != nil {
		t.Fatalf("mw grist timing --otlp failed: %v\n%s", err, out)
	}
	check(out)

	file := filepath.Join(t.TempDir(), "timing.json")
	if out, err := runMw(t, "grist", "timing", "--otlp", "--out", file); err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("--out wrote to stdout or failed: %v\n%s", err, out)
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	check(string(body))

	if out, err := runMw(t, "grist", "timing", "--out", file); err == nil || !strings.Contains(out, "--otlp") {
		t.Errorf("--out without --otlp should be refused naming --otlp: %v\n%s", err, out)
	}
}

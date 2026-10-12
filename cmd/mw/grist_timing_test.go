package main

import (
	"context"
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
		"wake", "pickup", "slot", "startup", "model", "deliver", "total", "seconds",
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

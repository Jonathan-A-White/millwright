package commandlog_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/commandlog"
)

func TestTheLogKeepsOneLineARunAndReadsBackTheRecentOnes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "commands.jsonl")
	log := commandlog.New(path)
	ctx := context.Background()
	if got, err := log.CommandRunsSince(ctx, time.Time{}); err != nil || len(got) != 0 {
		t.Fatalf("no log yet: %v %v", got, err)
	}
	old := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	for _, run := range []application.CommandRun{{At: old, Command: "mw old"}, {At: recent, Command: "mw check", Status: 1, DurationMs: 40}} {
		if err := log.AppendCommandRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "\n") != 2 {
		t.Errorf("expected two lines:\n%s", raw)
	}
	got, err := log.CommandRunsSince(ctx, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 1 || got[0].Command != "mw check" || got[0].Status != 1 || got[0].DurationMs != 40 {
		t.Errorf("read back: %+v %v", got, err)
	}
}

func TestALineThatIsNotARunIsSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.jsonl")
	if err := os.WriteFile(path, []byte("garbage\n{\"at\":\"2026-11-12T00:00:00Z\",\"command\":\"mw x\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := commandlog.New(path).CommandRunsSince(context.Background(), time.Time{})
	if err != nil || len(got) != 1 || got[0].Command != "mw x" {
		t.Errorf("read back: %+v %v", got, err)
	}
}

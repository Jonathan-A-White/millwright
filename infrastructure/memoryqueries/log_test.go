package memoryqueries_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/memoryqueries"
)

func TestTheLogKeepsOneLineAQueryAndReadsThemBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "memory-queries.jsonl")
	log := memoryqueries.New(path)
	ctx := context.Background()
	if got, err := log.ReadMemoryQueries(ctx); err != nil || len(got) != 0 {
		t.Fatalf("no log yet: %v %v", got, err)
	}
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, rig := range []string{"kiln", "oven"} {
		if err := log.AppendMemoryQuery(ctx, application.MemoryQueryRecord{At: at, Rig: rig, Terms: []string{"x"}, Hits: 1}); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(path)
	if strings.Count(string(raw), "\n") != 2 {
		t.Errorf("expected two lines:\n%s", raw)
	}
	got, err := log.ReadMemoryQueries(ctx)
	if err != nil || len(got) != 2 || got[1].Rig != "oven" || !got[0].At.Equal(at) {
		t.Errorf("read back: %+v %v", got, err)
	}
}

func TestALineThatIsNotAQueryIsSkippedNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.jsonl")
	if err := os.WriteFile(path, []byte("garbage\n{\"rig\":\"kiln\",\"hits\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := memoryqueries.New(path).ReadMemoryQueries(context.Background())
	if err != nil || len(got) != 1 || got[0].Rig != "kiln" {
		t.Errorf("got %+v %v", got, err)
	}
}

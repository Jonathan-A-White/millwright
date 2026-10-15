package eventlog

import (
	"context"
	"path/filepath"
	"testing"
)

func TestContextMarksAreKeptBesideTheLogAndReadBack(t *testing.T) {
	log := filepath.Join(t.TempDir(), "events", "log.jsonl")
	marks := NewContextMarks(log)
	ctx := context.Background()

	if got, err := marks.Load(ctx); err != nil || len(got) != 0 {
		t.Fatalf("with no file, expected no marks, got %v: %v", got, err)
	}
	if err := marks.Save(ctx, map[string]int{"deputy-2026-10-15-03": 205000}); err != nil {
		t.Fatal(err)
	}
	got, err := NewContextMarks(log).Load(ctx)
	if err != nil || got["deputy-2026-10-15-03"] != 205000 {
		t.Fatalf("expected 205000 back, got %v: %v", got, err)
	}
}

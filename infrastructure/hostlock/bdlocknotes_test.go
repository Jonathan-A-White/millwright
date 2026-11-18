package hostlock

import (
	"context"
	"testing"
)

func TestBdLockNotesKeepAndForgetANotePerStep(t *testing.T) {
	ctx := context.Background()
	notes := NewBdLockNotes(t.TempDir())
	if err := notes.Lose(ctx, []string{"f-mol-1.1", "f-mol-1.2"}, "bd (pid 7: bd list)"); err != nil {
		t.Fatal(err)
	}
	got, err := notes.Lost(ctx, []string{"f-mol-1.1", "f-mol-1.3", "../escape"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["f-mol-1.1"] != "bd (pid 7: bd list)" {
		t.Fatalf("expected the one note of f-mol-1.1, got %v", got)
	}
	if err := notes.Forget(ctx, []string{"f-mol-1.1", "f-mol-1.3"}); err != nil {
		t.Fatal(err)
	}
	got, _ = notes.Lost(ctx, []string{"f-mol-1.1", "f-mol-1.2"})
	if len(got) != 1 || got["f-mol-1.2"] == "" {
		t.Fatalf("expected only f-mol-1.2 left, got %v", got)
	}
}

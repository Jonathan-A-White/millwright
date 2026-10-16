package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLastWriteIsWhenTheNewestTranscriptWasWritten(t *testing.T) {
	root := t.TempDir()
	dir := "/root/.mw-worktrees/mw-gq6.30"
	older := time.Date(2026, 10, 16, 1, 1, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 16, 3, 28, 0, 0, time.UTC)
	writeTranscript(t, root, dir, "first", older, turn(1, 0, 0))
	writeTranscript(t, root, dir, "second", newer, turn(1, 0, 0))

	at, err := NewTranscripts(root).LastWrite(context.Background(), dir)
	if err != nil {
		t.Fatalf("reading the last write: %v", err)
	}
	if !at.Equal(newer) {
		t.Fatalf("expected the last write at %s, got %s", newer, at)
	}
}

// A subagent's transcript is kept under its session's own directory, and a
// session waiting on one writes nothing of its own until it answers.
func TestLastWriteCountsASubagentsTranscript(t *testing.T) {
	root := t.TempDir()
	dir := "/root/.mw-worktrees/mw-gq6.30"
	older := time.Date(2026, 10, 16, 1, 1, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 16, 3, 28, 0, 0, time.UTC)
	writeTranscript(t, root, dir, "main", older, turn(1, 0, 0))
	sub := filepath.Join(ProjectDir(root, dir), "main", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "agent-1.jsonl")
	if err := os.WriteFile(path, []byte(turn(1, 0, 0)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, newer, newer); err != nil {
		t.Fatal(err)
	}

	at, err := NewTranscripts(root).LastWrite(context.Background(), dir)
	if err != nil {
		t.Fatalf("reading the last write: %v", err)
	}
	if !at.Equal(newer) {
		t.Fatalf("expected the subagent's write at %s, got %s", newer, at)
	}
}

func TestLastWriteOfADirectoryWithNoTranscriptIsZeroNotAnError(t *testing.T) {
	at, err := NewTranscripts(t.TempDir()).LastWrite(context.Background(), "/nowhere")
	if err != nil || !at.IsZero() {
		t.Fatalf("expected the zero time and no error, got %s, %v", at, err)
	}
}

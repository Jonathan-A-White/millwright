package claude

import (
	"context"
	"testing"
	"time"
)

// A session's turns are counted as the harness counts num_turns: its prompt and
// each tool result after it, in its newest transcript, a subagent's left out
// (mw-1ljpo9.6).
func TestTurnsAreTheNewestTranscriptsMainChainUserEntries(t *testing.T) {
	root := t.TempDir()
	dir := "/home/jwhite/.mw-worktrees/mw-gq6.1"
	morning := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	toolResult := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`

	writeTranscript(t, root, dir, "11111111-old", morning, userLine, turn(1, 1, 1), toolResult, toolResult, toolResult)
	writeTranscript(t, root, dir, "22222222-new", morning.Add(time.Hour),
		userLine,
		turn(1, 1, 1),
		toolResult,
		turn(1, 1, 1),
		`{"isSidechain":true,"type":"user","message":{"role":"user","content":"a subagent's prompt"}}`,
		toolResult,
		`{"type":"user","message":{"role":"us`)

	got, err := NewTranscripts(root).Turns(context.Background(), dir)
	if err != nil {
		t.Fatalf("counting the turns: %v", err)
	}
	if got != 3 {
		t.Fatalf("expected the newest transcript's 3 turns, got %d", got)
	}
}

func TestTurnsOfADirectoryWithNoTranscriptAreNone(t *testing.T) {
	got, err := NewTranscripts(t.TempDir()).Turns(context.Background(), "/work/none")
	if err != nil || got != 0 {
		t.Fatalf("expected no turns and no error, got %d: %v", got, err)
	}
}

package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func costState(total string) string {
	return `{"type":"cost-state","sessionId":"x","totalCostUSD":` + total + `,"totalAPIDuration":1}`
}

func title(name string) string {
	return `{"type":"custom-title","customTitle":"` + name + `","sessionId":"x"}`
}

func TestSeatSessionsCostIsTheLastCostStateLine(t *testing.T) {
	root, dir := t.TempDir(), "/home/x/vault"
	now := time.Now()
	writeTranscript(t, root, dir, "finished", now, title("Mayor (after x)"), costState("1.25"), turn(1, 2, 3), costState("3.5"))
	writeTranscript(t, root, dir, "running", now, title("deputy-x"), turn(1, 2, 3))
	// A subagent's file lies under the session's directory and adds nothing.
	sub := filepath.Join(ProjectDir(root, dir), "finished", "subagents")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "agent-1.jsonl"), []byte(costState("9")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := NewSeatTranscripts(root, dir).SeatSessions(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][2]any{}
	for _, s := range got {
		byName[s.Name] = [2]any{s.CostUSD, s.HasCost}
	}
	if len(got) != 2 || byName["Mayor (after x)"] != [2]any{3.5, true} || byName["deputy-x"] != [2]any{0.0, false} {
		t.Fatalf("expected the finished session at 3.5 and the running one with no cost, got %+v", got)
	}
}

func TestSeatSessionsDoesNotOpenATranscriptModifiedBeforeTheWindows(t *testing.T) {
	root, dir := t.TempDir(), "/home/x/vault"
	now := time.Now()
	old := writeTranscript(t, root, dir, "old", now.AddDate(0, 0, -30), title("Mayor (old)"), costState("99"))
	// A file that cannot be opened proves nothing is read from it, unless the
	// tests run as root, which can open anything: the answer holds either way.
	_ = os.Chmod(old, 0)
	writeTranscript(t, root, dir, "recent", now.AddDate(0, 0, -1), title("Mayor (new)"), costState("2"))

	got, err := NewSeatTranscripts(root, dir).SeatSessions(context.Background(), now.AddDate(0, 0, -14))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Mayor (new)" || got[0].CostUSD != 2 {
		t.Fatalf("expected only the recent session, got %+v", got)
	}
}

func TestSeatSessionsReadsTheCostFromTheTailOfALargeTranscript(t *testing.T) {
	root, dir := t.TempDir(), "/home/x/vault"
	now := time.Now()
	filler := make([]byte, costTailBytes+4096)
	for i := range filler {
		filler[i] = 'a'
	}
	writeTranscript(t, root, dir, "big", now, title("Mayor (big)"), string(filler), costState("7.75"))
	got, err := NewSeatTranscripts(root, dir).SeatSessions(context.Background(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CostUSD != 7.75 || !got[0].HasCost {
		t.Fatalf("expected 7.75 from the tail, got %+v", got)
	}
}

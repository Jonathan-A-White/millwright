package claude

import (
	"context"
	"testing"
	"time"
)

// reply is one assistant line of a transcript, at a time.
func reply(at, extra, text string) string {
	return `{"type":"assistant","timestamp":"` + at + `",` + extra + `"message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}`
}

func TestRepliesAreTheNewestTranscriptsAssistantEntriesSinceTheTime(t *testing.T) {
	root := t.TempDir()
	dir := "/work/seat"
	morning := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)

	writeTranscript(t, root, dir, "old", morning, reply("2026-10-06T00:59:00Z", "", "from an older session"))
	writeTranscript(t, root, dir, "new", morning.Add(time.Hour), userLine,
		reply("2026-10-06T01:00:00Z", "", "too early"),
		reply("2026-10-06T01:14:32.5Z", `"isApiErrorMessage":true,`, "API Error: Connection lost mid-response\\nmore"),
		reply("2026-10-06T01:15:00Z", "", "API Error: ECONNREFUSED 127.0.0.1:8080"),
		reply("2026-10-06T01:16:00Z", `"isSidechain":true,`, "a subagent answered"),
		`{"type":"assistant","timestamp":"2026-10-06T01:17:00Z","message":{"model":"<synthetic>","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`,
		reply("2026-10-06T01:18:00Z", "", "Filed it."),
		`{"type":"assistant","timestamp":"2026-10-06T01:19:0`)

	got, err := NewTranscripts(root).Replies(context.Background(), dir, time.Date(2026, 10, 6, 1, 10, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Replies: %v", err)
	}
	want := []struct {
		text     string
		apiError bool
	}{
		{"API Error: Connection lost mid-response", true},
		{"API Error: ECONNREFUSED 127.0.0.1:8080", true},
		{"Filed it.", false},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d replies, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].Text != w.text || got[i].APIError != w.apiError {
			t.Errorf("reply %d: expected %q (api error %v), got %q (api error %v)", i, w.text, w.apiError, got[i].Text, got[i].APIError)
		}
	}
	if !got[0].At.Equal(time.Date(2026, 10, 6, 1, 14, 32, 500_000_000, time.UTC)) {
		t.Errorf("expected the first reply at 01:14:32.5, got %s", got[0].At)
	}
}

func TestRepliesOfADirectoryWithNoTranscriptAreNone(t *testing.T) {
	got, err := NewTranscripts(t.TempDir()).Replies(context.Background(), "/work/seat", time.Time{})
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no replies and no error, got %+v, %v", got, err)
	}
}

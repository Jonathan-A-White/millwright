package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Transcripts satisfies the tail port too.
var _ application.TranscriptTail = (*Transcripts)(nil)

// maxTailLine is the longest an entry is shown: a tool result can be pages.
const maxTailLine = 200

// entry is the part of a transcript line Tail shows.
type entry struct {
	IsSidechain bool   `json:"isSidechain"`
	Type        string `json:"type"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block is one part of a message's content.
type block struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

// Tail implements application.TranscriptTail: the newest session run in dir as
// one line per thing said or done — what the model wrote, the tool it ran and
// what that printed — the most recent last. A directory with no transcript
// reads as "": nothing has been recorded, which is not an error. Lines that are
// not JSON are skipped, as the last of a session still being written may be cut
// short, and so are a subagent's.
func (t *Transcripts) Tail(_ context.Context, dir string, lines int) (string, error) {
	newest := newestTranscript(ProjectDir(t.root, dir))
	if newest == "" {
		return "", nil
	}
	file, err := os.Open(newest)
	if err != nil {
		return "", fmt.Errorf("reading the transcript %s: %w", newest, err)
	}
	defer file.Close()

	var shown []string
	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			var seen entry
			if json.Unmarshal(raw, &seen) == nil && !seen.IsSidechain && (seen.Type == "user" || seen.Type == "assistant") {
				shown = append(shown, describe(seen)...)
			}
		}
		if readErr != nil {
			break
		}
	}
	return application.RecentLines(strings.Join(shown, "\n"), lines), nil
}

// describe is the lines one transcript entry shows.
func describe(e entry) []string {
	var text string
	if json.Unmarshal(e.Message.Content, &text) == nil {
		return oneLine(e.Type, text)
	}
	var blocks []block
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, oneLine(e.Type, b.Text)...)
		case "tool_use":
			out = append(out, oneLine("tool "+b.Name, string(b.Input))...)
		case "tool_result":
			var result string
			if json.Unmarshal(b.Content, &result) != nil {
				result = string(b.Content)
			}
			out = append(out, oneLine("result", result)...)
		}
	}
	return out
}

// oneLine is who said what on a single line, cut to maxTailLine; nothing when
// there was nothing said.
func oneLine(who, said string) []string {
	said = strings.Join(strings.Fields(said), " ")
	if said == "" {
		return nil
	}
	line := who + ": " + said
	if runes := []rune(line); len(runes) > maxTailLine {
		line = string(runes[:maxTailLine]) + "…"
	}
	return []string{line}
}

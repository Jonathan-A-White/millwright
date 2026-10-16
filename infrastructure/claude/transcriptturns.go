package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
)

// Transcripts satisfies the turns port too.
var _ application.TranscriptTurns = (*Transcripts)(nil)

// turnEntry is the part of a transcript line Turns reads.
type turnEntry struct {
	IsSidechain bool   `json:"isSidechain"`
	Type        string `json:"type"`
}

// Turns implements application.TranscriptTurns: the main-chain user entries of
// the newest session run in dir — the prompt and every tool result after it —
// which is how the harness counts the num_turns of its result and its
// --max-turns (mw-1ljpo9.6). A subagent's entries are not this session's turns,
// and a line that is not JSON, the last one cut short, is skipped.
func (t *Transcripts) Turns(_ context.Context, dir string) (int, error) {
	newest := newestTranscript(ProjectDir(t.root, dir))
	if newest == "" {
		return 0, nil
	}
	file, err := os.Open(newest)
	if err != nil {
		return 0, fmt.Errorf("reading the transcript %s: %w", newest, err)
	}
	defer file.Close()

	turns := 0
	lines := bufio.NewReaderSize(file, 1<<20)
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var seen turnEntry
			if json.Unmarshal(line, &seen) == nil && seen.Type == "user" && !seen.IsSidechain {
				turns++
			}
		}
		if readErr != nil {
			return turns, nil
		}
	}
}

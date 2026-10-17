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

// Transcripts satisfies the permission-mode port too.
var _ application.TranscriptModes = (*Transcripts)(nil)

// modeEntry is the part of a transcript line PermissionMode reads.
type modeEntry struct {
	PermissionMode string `json:"permissionMode"`
}

// PermissionMode implements application.TranscriptModes: the permission mode the
// newest session run in dir wrote in its transcript (mw-gq6.545). A mode that is
// not auto is named as soon as one entry shows it; otherwise the answer is auto
// if any entry shows it, and empty when none does. A line that is not JSON, the
// last one cut short, is skipped.
func (t *Transcripts) PermissionMode(_ context.Context, dir string) (string, error) {
	newest := newestTranscript(ProjectDir(t.root, dir))
	if newest == "" {
		return "", nil
	}
	file, err := os.Open(newest)
	if err != nil {
		return "", fmt.Errorf("reading the transcript %s: %w", newest, err)
	}
	defer file.Close()

	seen := ""
	lines := bufio.NewReaderSize(file, 1<<20)
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var entry modeEntry
			if json.Unmarshal(line, &entry) == nil && entry.PermissionMode != "" {
				if entry.PermissionMode != PermissionAuto {
					return entry.PermissionMode, nil
				}
				seen = entry.PermissionMode
			}
		}
		if readErr != nil {
			return seen, nil
		}
	}
}

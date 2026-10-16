package claude

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Transcripts satisfies the quiet alarm's port too.
var _ application.TranscriptWrites = (*Transcripts)(nil)

// LastWrite implements application.TranscriptWrites: the newest modification
// time of any transcript kept for sessions run in dir, a subagent's included —
// Claude Code keeps those under the session's own directory, and a session
// waiting on one writes nothing of its own until it answers. A directory with
// no transcript reads as the zero time, which is not an error.
func (t *Transcripts) LastWrite(_ context.Context, dir string) (time.Time, error) {
	var newest time.Time
	// A file that vanishes or cannot be read mid-walk is passed over: the
	// answer is the newest write that can be seen.
	_ = filepath.WalkDir(ProjectDir(t.root, dir), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if at := info.ModTime(); at.After(newest) {
			newest = at
		}
		return nil
	})
	return newest, nil
}

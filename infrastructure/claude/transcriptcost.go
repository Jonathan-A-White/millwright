package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// costTailBytes is how much of the end of a transcript is read for its cost: a
// finished session holds one cost-state line at the tail, a few hundred bytes.
const costTailBytes = 1 << 20

// SeatTranscripts reads the transcripts of the sessions run in one directory
// (the vault's, where the Mayor and the other seats run) for what each cost. It
// is the adapter behind application.SeatSessions; it reads and never writes.
type SeatTranscripts struct {
	root, dir string
}

var _ application.SeatSessions = (*SeatTranscripts)(nil)

// NewSeatTranscripts returns the adapter reading, under the projects root, the
// transcripts of sessions run in dir.
func NewSeatTranscripts(root, dir string) *SeatTranscripts {
	return &SeatTranscripts{root: root, dir: dir}
}

// SeatSessions implements application.SeatSessions: each main session of the
// directory whose transcript was modified at or after since, named by its
// custom-title line, with the totalCostUSD of the last cost-state line. A
// subagent's file lies under <session>/subagents/ and is not listed. A
// transcript modified before since is not opened.
func (s *SeatTranscripts) SeatSessions(ctx context.Context, since time.Time) ([]application.SeatSession, error) {
	found, _ := filepath.Glob(filepath.Join(ProjectDir(s.root, s.dir), "*.jsonl"))
	var sessions []application.SeatSession
	for _, path := range found {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.ModTime().Before(since) {
			continue
		}
		session := application.SeatSession{Name: titleOf(path), At: info.ModTime()}
		session.CostUSD, session.HasCost = lastCost(path)
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// lastCost is the totalCostUSD of the last cost-state line in the tail of the
// transcript at path, and whether it has one: a running session has none yet.
func lastCost(path string) (float64, bool) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, false
	}
	start := max(info.Size()-costTailBytes, 0)
	tail := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(tail, start); err != nil && err != io.EOF {
		return 0, false
	}
	lines := bytes.Split(tail, []byte("\n"))
	if start > 0 {
		// The first line of the tail may have been cut in two.
		lines = lines[1:]
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"cost-state"`)) {
			continue
		}
		var seen struct {
			Type string   `json:"type"`
			Cost *float64 `json:"totalCostUSD"`
		}
		if json.Unmarshal(lines[i], &seen) == nil && seen.Type == "cost-state" && seen.Cost != nil {
			return *seen.Cost, true
		}
	}
	return 0, false
}

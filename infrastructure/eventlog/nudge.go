package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// NudgeFile is where the follower keeps the last seq each seat was told of,
// by its name in the log's directory.
const NudgeFile = "nudge.json"

// NudgeCursors keeps the follower's per-seat cursors in NudgeFile beside the
// log.
type NudgeCursors struct {
	Path string
}

var _ application.NudgeCursors = (*NudgeCursors)(nil)

// NewNudgeCursors is the cursors kept beside the log in the file logPath.
func NewNudgeCursors(logPath string) *NudgeCursors {
	return &NudgeCursors{Path: filepath.Join(filepath.Dir(logPath), NudgeFile)}
}

// Load implements application.NudgeCursors; no file is no cursors.
func (c *NudgeCursors) Load(context.Context) (map[string]uint64, error) {
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]uint64{}, nil
	}
	if err != nil {
		return nil, err
	}
	cursors := map[string]uint64{}
	if err := json.Unmarshal(data, &cursors); err != nil {
		return nil, fmt.Errorf("reading the seats' nudge cursors %s: %w", c.Path, err)
	}
	return cursors, nil
}

// Save implements application.NudgeCursors, replacing the file whole.
func (c *NudgeCursors) Save(_ context.Context, cursors map[string]uint64) error {
	data, err := json.Marshal(cursors)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	return writeSynced(c.Path, data)
}

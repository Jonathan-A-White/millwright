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

// CursorFile is the follower's cursor, by its name in the log's directory.
const CursorFile = "follow.json"

// Cursors keeps the follower's cursor in CursorFile beside the log.
type Cursors struct {
	Path string
}

// Cursors satisfies the port.
var _ application.FollowCursors = (*Cursors)(nil)

// NewCursors is the cursor kept beside the log in the file logPath.
func NewCursors(logPath string) *Cursors {
	return &Cursors{Path: filepath.Join(filepath.Dir(logPath), CursorFile)}
}

// Load implements application.FollowCursors.
func (c *Cursors) Load(context.Context) (application.FollowCursor, bool, error) {
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return application.FollowCursor{}, false, nil
	}
	if err != nil {
		return application.FollowCursor{}, false, err
	}
	var cursor application.FollowCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return application.FollowCursor{}, false, fmt.Errorf("reading the follower's cursor %s: %w", c.Path, err)
	}
	return cursor, true, nil
}

// Save implements application.FollowCursors, replacing the file whole.
func (c *Cursors) Save(_ context.Context, cursor application.FollowCursor) error {
	data, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	return writeSynced(c.Path, data)
}

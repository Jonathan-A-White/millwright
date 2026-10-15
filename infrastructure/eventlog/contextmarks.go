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

// ContextMarksFile is the file the seat-context watcher keeps what it told
// each window in, by its name in the log's directory.
const ContextMarksFile = "context-marks.json"

// ContextMarks keeps the watcher's marks in ContextMarksFile beside the log.
type ContextMarks struct {
	Path string
}

var _ application.ContextMarks = (*ContextMarks)(nil)

// NewContextMarks is the marks kept beside the log in the file logPath.
func NewContextMarks(logPath string) *ContextMarks {
	return &ContextMarks{Path: filepath.Join(filepath.Dir(logPath), ContextMarksFile)}
}

// Load implements application.ContextMarks; no file is no marks.
func (c *ContextMarks) Load(context.Context) (map[string]int, error) {
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	marks := map[string]int{}
	if err := json.Unmarshal(data, &marks); err != nil {
		return nil, fmt.Errorf("reading the seats' context marks %s: %w", c.Path, err)
	}
	return marks, nil
}

// Save implements application.ContextMarks, replacing the file whole.
func (c *ContextMarks) Save(_ context.Context, marks map[string]int) error {
	data, err := json.Marshal(marks)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	return writeSynced(c.Path, data)
}

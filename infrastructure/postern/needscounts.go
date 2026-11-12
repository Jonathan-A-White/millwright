package postern

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.NeedsCountsReader = (*NeedsCountsFile)(nil)

// NeedsCountsFile is the plaintext counts file the view writes beside itself.
type NeedsCountsFile struct {
	path string
}

// NewNeedsCountsFile is the counts file at path.
func NewNeedsCountsFile(path string) *NeedsCountsFile { return &NeedsCountsFile{path: path} }

// ReadNeedsCounts implements application.NeedsCountsReader: the file's bytes,
// nil with no error when it is not there.
func (f *NeedsCountsFile) ReadNeedsCounts(context.Context) ([]byte, error) {
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.path, err)
	}
	if raw == nil {
		raw = []byte{}
	}
	return raw, nil
}

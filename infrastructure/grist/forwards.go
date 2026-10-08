package grist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// ForwardsDir is the directory of the state directory holding the pictures of
// every forwarded grist, one directory for each, named by its grist's txid.
const ForwardsDir = "forwarded"

// Forwards keeps the pictures of a forwarded grist under Dir/forwarded/<txid>/
// (application's GristForwardStore). Nothing it writes is ever deleted by mw.
// They are private: 0700 directories, 0600 files.
type Forwards struct {
	Dir string
}

// Forwards satisfies the port.
var _ application.GristForwardStore = (*Forwards)(nil)

// NewForwards is the pictures kept under the state directory dir.
func NewForwards(dir string) *Forwards { return &Forwards{Dir: dir} }

// Keep implements application.GristForwardStore: picture-N.<ext>, N from 1,
// written again if an earlier try left them there. It reports each full path.
func (f *Forwards) Keep(_ context.Context, txid string, pictures []application.GristRunAttachment) ([]string, error) {
	dir := filepath.Join(f.Dir, ForwardsDir, application.GristRunName(txid))
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("making %s: %w", dir, err)
	}
	paths := make([]string, 0, len(pictures))
	for i, p := range pictures {
		path := filepath.Join(dir, fmt.Sprintf("picture-%d%s", i+1, p.Ext))
		if err := os.WriteFile(path, p.Data, 0o600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", path, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

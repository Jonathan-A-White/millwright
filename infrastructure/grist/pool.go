package grist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// PoolFile is what the follower's warm pool holds, kind by kind, written
// whole each time it changes, for mw status to read.
const PoolFile = "pool.json"

// State keeps the pool's board too.
var _ application.GristPoolBoard = (*State)(nil)

// SavePool implements application.GristPoolBoard.
func (s *State) SavePool(_ context.Context, view application.GristPoolView) error {
	data, err := json.Marshal(view)
	if err != nil {
		return err
	}
	return s.writeWhole(filepath.Join(s.Dir, PoolFile), append(data, '\n'))
}

// Pool implements application.GristPoolBoard: no file is no view.
func (s *State) Pool(context.Context) (application.GristPoolView, error) {
	path := filepath.Join(s.Dir, PoolFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return application.GristPoolView{}, nil
	}
	if err != nil {
		return application.GristPoolView{}, fmt.Errorf("reading the warm pool's board: %w", err)
	}
	var view application.GristPoolView
	if err := json.Unmarshal(raw, &view); err != nil {
		return application.GristPoolView{}, fmt.Errorf("the warm pool's board %s is not JSON: %w", path, err)
	}
	return view, nil
}

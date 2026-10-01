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

// ShipFile is the shipper's state, by its name in the log's directory.
const ShipFile = "ship.json"

// ShipStates keeps the shipper's state in ShipFile beside the log.
type ShipStates struct {
	Path string
}

// ShipStates satisfies the port.
var _ application.ShipStates = (*ShipStates)(nil)

// NewShipStates is the state kept beside the log in the file logPath.
func NewShipStates(logPath string) *ShipStates {
	return &ShipStates{Path: filepath.Join(filepath.Dir(logPath), ShipFile)}
}

// Load implements application.ShipStates: the zero state when there is no file.
func (s *ShipStates) Load(context.Context) (application.ShipState, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return application.ShipState{}, nil
	}
	if err != nil {
		return application.ShipState{}, err
	}
	var state application.ShipState
	if err := json.Unmarshal(data, &state); err != nil {
		return application.ShipState{}, fmt.Errorf("reading the shipper's state %s: %w", s.Path, err)
	}
	return state, nil
}

// Save implements application.ShipStates, replacing the file whole.
func (s *ShipStates) Save(_ context.Context, state application.ShipState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	return writeSynced(s.Path, data)
}

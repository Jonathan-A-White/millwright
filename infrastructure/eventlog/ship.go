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

// ShipArchive keeps the ranges mw events retire gives up in a file for each
// UTC day beside ShipFile: ship-retired-<date>.json, whose "pending" list is in
// the shape of ship.json's, so putting it back there sends them again.
type ShipArchive struct {
	Dir string
}

// ShipArchive satisfies the port.
var _ application.ShipArchive = (*ShipArchive)(nil)

// NewShipArchive is the archive kept beside the log in the file logPath.
func NewShipArchive(logPath string) *ShipArchive {
	return &ShipArchive{Dir: filepath.Dir(logPath)}
}

// retiredFile is the archive file of one day's retire.
type retiredFile struct {
	Pending []application.ShipRange `json:"pending"`
}

// Path implements application.ShipArchive.
func (a *ShipArchive) Path(day string) string {
	return filepath.Join(a.Dir, "ship-retired-"+day+".json")
}

// Keep implements application.ShipArchive: the file's ranges, then those of
// ranges it does not hold, written to a synced file renamed over it.
func (a *ShipArchive) Keep(_ context.Context, ranges []application.ShipRange, day string) (string, error) {
	path := a.Path(day)
	var file retiredFile
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &file); err != nil {
			return "", fmt.Errorf("reading the archive %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	held := make(map[application.ShipRange]bool, len(file.Pending)+len(ranges))
	for _, r := range file.Pending {
		held[r] = true
	}
	for _, r := range ranges {
		if !held[r] {
			held[r] = true
			file.Pending = append(file.Pending, r)
		}
	}
	if data, err = json.Marshal(file); err != nil {
		return "", err
	}
	if err := os.MkdirAll(a.Dir, 0o700); err != nil {
		return "", err
	}
	if err := writeSynced(path, data); err != nil {
		return "", err
	}
	return path, nil
}

package network

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
)

// File is the memory's name inside its directory.
const File = "network.json"

// Store is the host's memory of the network, a file in Dir, replaced whole so
// that a write never leaves half of one.
type Store struct {
	Dir string
}

var _ application.NetworkStore = Store{}

// NewStore is the memory kept in dir.
func NewStore(dir string) Store { return Store{Dir: dir} }

// Load implements application.NetworkStore. A missing file, or one that cannot
// be read as one, is a memory of nothing, not a failure.
func (s Store) Load(context.Context) (application.NetworkMemory, error) {
	held, err := os.ReadFile(filepath.Join(s.Dir, File))
	if os.IsNotExist(err) {
		return application.NetworkMemory{}, nil
	}
	if err != nil {
		return application.NetworkMemory{}, fmt.Errorf("reading %s: %w", filepath.Join(s.Dir, File), err)
	}
	var memory application.NetworkMemory
	if json.Unmarshal(held, &memory) != nil {
		return application.NetworkMemory{}, nil
	}
	return memory, nil
}

// Save implements application.NetworkStore.
func (s Store) Save(_ context.Context, memory application.NetworkMemory) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", s.Dir, err)
	}
	body, err := json.Marshal(memory)
	if err != nil {
		return err
	}
	path := filepath.Join(s.Dir, File)
	return atomicfile.Write(path, body, 0o644)
}

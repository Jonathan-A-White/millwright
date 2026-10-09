package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

// CloudBookPath is where the cloud's book is kept in the vault, beside the
// Vultr box's settings, state and keys.
const CloudBookPath = "hosts/vultr/cloud.json"

// CloudBook is the cloud's book as a JSON file in the vault, committed on
// each write so that the month's spend travels with the vault to the next
// home.
type CloudBook struct{ Vault *Vault }

var _ application.CloudBook = CloudBook{}

// Read implements application.CloudBook: a vault with no book holds the zero
// state.
func (b CloudBook) Read(context.Context) (application.CloudState, error) {
	var state application.CloudState
	raw, err := os.ReadFile(filepath.Join(b.Vault.dir, CloudBookPath))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, fmt.Errorf("reading %s in the vault: %w", CloudBookPath, err)
	}
	return state, nil
}

// Write implements application.CloudBook: the file is replaced whole, then
// committed alone, the commit message saying why.
func (b CloudBook) Write(ctx context.Context, state application.CloudState, why string) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(b.Vault.dir, CloudBookPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	_, err = b.Vault.Commit(ctx, "mw cloud: "+why, []string{CloudBookPath})
	return err
}

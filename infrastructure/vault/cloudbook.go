package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// CloudBookPath is where the cloud's book is kept in the vault, beside the
// Vultr box's settings, state and keys.
const CloudBookPath = "hosts/vultr/cloud.json"

// cloudBookCommitWait bounds the book's commit once it is begun: long enough
// for a local git commit on a loaded box, short enough that a git stuck on a
// lock does not hold a stopped mw for ever.
const cloudBookCommitWait = 2 * time.Minute

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
//
// The write and its commit are one step: once the file is replaced, the
// commit runs to its end even when ctx has ended (mw told to stop, a caller's
// timeout), since a book written and not committed is a tracked file every
// host's dispatch refuses on until someone commits it (mw-gq6.462). Killing
// git part-way through its commit would be worse still: it leaves the
// index locked.
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
	commit, cancel := context.WithTimeout(context.WithoutCancel(ctx), cloudBookCommitWait)
	defer cancel()
	_, err = b.Vault.Commit(commit, "mw cloud: "+why, []string{CloudBookPath})
	return err
}

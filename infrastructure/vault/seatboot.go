package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Vault is the adapter behind application.SeatBootFiles as well: this file
// reads a seat's boot manifest and the vault files it names, for `mw seat
// boot` and `mw seat read`.
var _ application.SeatBootFiles = (*Vault)(nil)

// SeatManifest implements application.SeatBootFiles: seats/<seat>/boot.toml,
// and whether the seat keeps one.
func (v *Vault) SeatManifest(_ context.Context, seat string) (string, bool, error) {
	if err := safeName("seat", seat); err != nil {
		return "", false, err
	}
	text, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, application.SeatManifestFileName))
	switch {
	case os.IsNotExist(err):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("reading the %s seat's %s: %w", seat, application.SeatManifestFileName, err)
	}
	return string(text), true, nil
}

// VaultFile implements application.SeatBootFiles: one file by path from the
// vault's root.
func (v *Vault) VaultFile(_ context.Context, rel string) (string, error) {
	if err := safeRel(rel); err != nil {
		return "", err
	}
	text, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("reading %s in the vault: %w", rel, err)
	}
	return string(text), nil
}

// NewestVaultFile implements application.SeatBootFiles: the newest .md file,
// by name, of a directory of the vault — the way a seat's handoffs are named,
// by date and number, the last by name is the newest.
func (v *Vault) NewestVaultFile(_ context.Context, dir string) (string, string, error) {
	if err := safeRel(dir); err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(filepath.Join(v.dir, filepath.FromSlash(dir)))
	if err != nil {
		return "", "", fmt.Errorf("listing %s in the vault: %w", dir, err)
	}
	newest := ""
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), application.HandoffExt) {
			newest = entry.Name()
		}
	}
	if newest == "" {
		return "", "", fmt.Errorf("%s in the vault holds no %s file", dir, application.HandoffExt)
	}
	name := path.Join(dir, newest)
	text, err := os.ReadFile(filepath.Join(v.dir, filepath.FromSlash(name)))
	if err != nil {
		return "", "", fmt.Errorf("reading %s in the vault: %w", name, err)
	}
	return name, string(text), nil
}

// safeRel is why a path cannot be read from a root, or nil: it is relative,
// and nothing in it climbs above the root.
func safeRel(rel string) error {
	switch {
	case rel == "":
		return fmt.Errorf("a path is needed")
	case strings.HasPrefix(rel, "/"), strings.Contains(rel, `\`), strings.Contains(rel, ".."):
		return fmt.Errorf("%q would reach outside the vault", rel)
	}
	return nil
}

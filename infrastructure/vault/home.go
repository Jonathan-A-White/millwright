package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

var (
	_ application.HomeFile   = (*Vault)(nil)
	_ application.HomeWriter = (*Vault)(nil)
)

// ReadHome implements application.HomeFile: the text of `home` at the top of the
// vault, or application.ErrNoHomeFile when there is none.
func (v *Vault) ReadHome(_ context.Context) (string, error) {
	text, err := os.ReadFile(filepath.Join(v.dir, application.HomeFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return "", application.ErrNoHomeFile
	}
	if err != nil {
		return "", fmt.Errorf("reading the vault's home file: %w", err)
	}
	return string(text), nil
}

// WriteHome implements application.HomeWriter: the home file, replaced whole. It
// is written beside the file and renamed over it, so that a reader never sees half
// a line. It commits nothing.
func (v *Vault) WriteHome(_ context.Context, record domain.HomeRecord) error {
	path := filepath.Join(v.dir, application.HomeFileName)
	temp, err := os.CreateTemp(v.dir, ".home-*")
	if err != nil {
		return fmt.Errorf("writing the vault's home file: %w", err)
	}
	defer os.Remove(temp.Name())
	if _, err := temp.WriteString(record.String()); err != nil {
		temp.Close()
		return fmt.Errorf("writing the vault's home file: %w", err)
	}
	if err := temp.Chmod(0o644); err != nil {
		temp.Close()
		return fmt.Errorf("writing the vault's home file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("writing the vault's home file: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("writing the vault's home file: %w", err)
	}
	return nil
}

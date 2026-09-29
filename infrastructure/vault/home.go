package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.HomeFile = (*Vault)(nil)

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

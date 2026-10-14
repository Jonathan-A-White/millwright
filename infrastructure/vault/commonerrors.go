package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// errorRegister is the folder of a seat's common-errors register.
func (v *Vault) errorRegister(seat string) (string, error) {
	if err := safeName("seat", seat); err != nil {
		return "", err
	}
	return filepath.Join(v.dir, SeatsDir, seat, application.ErrorRegisterDir), nil
}

// ReadErrorRegister implements application.ErrorFiles: the text of every .md
// file of the seat's register by slug, and the text of its kinds file. A seat
// with no register has neither, which is not an error.
func (v *Vault) ReadErrorRegister(_ context.Context, seat string) (map[string]string, string, error) {
	dir, err := v.errorRegister(seat)
	if err != nil {
		return nil, "", err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return map[string]string{}, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("reading the %s seat's error register: %w", seat, err)
	}
	classes := map[string]string{}
	for _, entry := range entries {
		slug, isClass := strings.CutSuffix(entry.Name(), application.MemoryExt)
		if entry.IsDir() || !isClass {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, "", fmt.Errorf("reading the error class %s: %w", slug, err)
		}
		classes[slug] = string(text)
	}
	kinds, err := os.ReadFile(filepath.Join(dir, application.ErrorKindsFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("reading the error register's %s: %w", application.ErrorKindsFile, err)
	}
	return classes, string(kinds), nil
}

// WriteErrorClass implements application.ErrorFiles.
func (v *Vault) WriteErrorClass(_ context.Context, seat, slug, text string) (string, error) {
	if err := safeName("error class", slug); err != nil {
		return "", err
	}
	return v.writeErrorFile(seat, slug+application.MemoryExt, text)
}

// WriteErrorKinds implements application.ErrorFiles.
func (v *Vault) WriteErrorKinds(_ context.Context, seat, text string) (string, error) {
	return v.writeErrorFile(seat, application.ErrorKindsFile, text)
}

func (v *Vault) writeErrorFile(seat, name, text string) (string, error) {
	dir, err := v.errorRegister(seat)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("making the %s seat's error register: %w", seat, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

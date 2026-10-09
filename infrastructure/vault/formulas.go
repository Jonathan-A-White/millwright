package vault

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Formulas is the vault's .beads/formulas, the directory bd pours the factory's
// formulas from, kept level with the factory rig's formulas/ (mw-gq6.314).
type Formulas struct {
	vault *Vault
}

var _ application.FormulaInstaller = (*Formulas)(nil)

// NewFormulas returns the formulas held in v.
func NewFormulas(v *Vault) *Formulas { return &Formulas{vault: v} }

// factoryFormulas lists the formula files of the factory checkout at dir, by
// file name.
func factoryFormulas(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, application.FactoryFormulasDir, "*.formula.json"))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)
	return names, nil
}

// OutOfStep implements application.FormulaInstaller.
func (f *Formulas) OutOfStep(_ context.Context, factoryDir string) ([]string, error) {
	names, err := factoryFormulas(factoryDir)
	if err != nil {
		return nil, err
	}
	var stale []string
	for _, name := range names {
		want, err := os.ReadFile(filepath.Join(factoryDir, application.FactoryFormulasDir, name))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		have, err := os.ReadFile(filepath.Join(f.vault.dir, application.FormulasDir, name))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading the vault's %s: %w", name, err)
		}
		if err != nil || !bytes.Equal(have, want) {
			stale = append(stale, name)
		}
	}
	return stale, nil
}

// Held implements application.FormulaInstaller.
func (f *Formulas) Held(ctx context.Context) ([]string, error) {
	changed, err := f.vault.Uncommitted(ctx)
	if err != nil {
		return nil, err
	}
	var held []string
	for _, path := range changed {
		if strings.HasPrefix(path, application.FormulasDir+"/") {
			held = append(held, path)
		}
	}
	return held, nil
}

// Install implements application.FormulaInstaller. The commit is the vault's
// own, made with the Vault's author, and records only the formula files.
func (f *Formulas) Install(ctx context.Context, factoryDir, message string) ([]string, string, error) {
	stale, err := f.OutOfStep(ctx, factoryDir)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Join(f.vault.dir, application.FormulasDir), 0o755); err != nil {
		return nil, "", err
	}
	paths := make([]string, 0, len(stale))
	for _, name := range stale {
		body, err := os.ReadFile(filepath.Join(factoryDir, application.FactoryFormulasDir, name))
		if err != nil {
			return nil, "", fmt.Errorf("reading %s: %w", name, err)
		}
		dest := filepath.Join(f.vault.dir, application.FormulasDir, name)
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return nil, "", fmt.Errorf("writing the vault's %s: %w", name, err)
		}
		paths = append(paths, application.FormulasDir+"/"+name)
	}
	committed, err := f.vault.Commit(ctx, message, paths)
	if err != nil {
		return nil, "", err
	}
	if len(committed) == 0 {
		return nil, "", nil
	}
	if _, err := f.vault.Push(ctx); err != nil {
		return committed, "committed here; not pushed yet, the next mw sync will: " + firstLineOf(err.Error()), nil
	}
	return committed, "", nil
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

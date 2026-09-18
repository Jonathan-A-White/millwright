// Package vault reads the factory's seats out of the vault directory and
// writes each story's run into it. It is the adapter behind application.Vault.
//
// The vault is laid out as ADR 0003 describes it:
//
//	seats/<seat>/charter.md        always read at boot
//	seats/<seat>/rigs/<rig>.md     read only for the rig being worked
//	seats/<seat>/ledger.md         never read at boot
//	seats/<seat>/postmortems/      never read at boot
//	runs/<story-id>/               one directory per story worked
//
// This package reads the first two and nothing else: everything a session is
// primed with is paid for on every story, so what is not needed is not read.
package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// The names of the parts of a seat this package reads.
const (
	CharterFile = "charter.md"
	SeatsDir    = "seats"
	RigsDir     = "rigs"
	RunsDir     = "runs"
)

// Vault is one vault directory.
type Vault struct {
	dir string
}

// New returns the vault held in a directory.
func New(dir string) *Vault { return &Vault{dir: dir} }

// Vault satisfies the port.
var _ application.Vault = (*Vault)(nil)

// Dir reports the directory this vault is held in.
func (v *Vault) Dir() string { return v.dir }

// Seat implements application.Vault: it reads the seat's charter and, if the
// seat has one, its memory of the rig. A seat with no memory of that rig is
// not an error; a seat with no charter is.
func (v *Vault) Seat(_ context.Context, seat, rig string) (application.Seat, error) {
	if err := safeName("seat", seat); err != nil {
		return application.Seat{}, err
	}
	charter, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, CharterFile))
	if err != nil {
		return application.Seat{}, fmt.Errorf("reading the %s seat's charter: %w", seat, err)
	}

	read := application.Seat{Name: seat, Charter: string(charter), Rig: rig}
	if rig == "" {
		return read, nil
	}
	if err := safeName("rig", rig); err != nil {
		return application.Seat{}, err
	}
	memory, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, RigsDir, rig+".md"))
	switch {
	case os.IsNotExist(err):
		// A seat that has not worked this rig yet boots without a memory of it.
		return read, nil
	case err != nil:
		return application.Seat{}, fmt.Errorf("reading the %s seat's memory of the rig %s: %w", seat, rig, err)
	}
	read.Memory = string(memory)
	return read, nil
}

// RunFile implements application.Vault.
func (v *Vault) RunFile(storyID, name string) string {
	return filepath.Join(v.dir, RunsDir, storyID, name)
}

// PutRunFile implements application.Vault.
func (v *Vault) PutRunFile(_ context.Context, storyID, name, contents string) (string, error) {
	if err := safeName("story", storyID); err != nil {
		return "", err
	}
	if err := safeName("run file", name); err != nil {
		return "", err
	}
	path := v.RunFile(storyID, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("making the run directory of %s: %w", storyID, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// safeName refuses a name that would reach outside the vault. Seat, rig and
// story names all end up in a path, and every one of them comes from a bead or
// a config file rather than from this package.
func safeName(what, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("a %s needs a name", what)
	case name == "." || name == "..",
		strings.ContainsAny(name, `/\`),
		strings.Contains(name, ".."):
		return fmt.Errorf("%q is not a %s name: it would reach outside the vault", name, what)
	}
	return nil
}

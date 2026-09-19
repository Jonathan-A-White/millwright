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
//
// The vault is also a git clone that both hosts commit to, so this package is
// the adapter behind application.VaultFiles as well: git.go keeps the clone
// level with the other host's.
package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// The names of the parts of a seat this package reads. The three a close-out has
// to name by path when it commits are the application's, so that the layout is
// written down once.
const (
	CharterFile = "charter.md"
	SeatsDir    = application.SeatsDir
	RigsDir     = application.RigsDir
	RunsDir     = application.RunsDir
)

// Vault is one vault directory.
type Vault struct {
	dir    string
	author string
}

// Option shapes a Vault.
type Option func(*Vault)

// WithAuthor names who the commits this Vault makes are recorded under, as both
// author and committer, name and email alike: mw@<host>, the name mw acts under
// in the tracker (ADR 0005). It is given to git per command, with -c, and never
// written into the clone's config, so a commit made by hand in the same clone
// is still the person's. A Vault with no author leaves git to the clone's own.
func WithAuthor(author string) Option {
	return func(v *Vault) { v.author = strings.TrimSpace(author) }
}

// New returns the vault held in a directory.
func New(dir string, opts ...Option) *Vault {
	v := &Vault{dir: dir}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

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
	memory, err := os.ReadFile(filepath.Join(v.dir, SeatsDir, seat, RigsDir, rig+application.MemoryExt))
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

// ReadRunFile implements application.Vault. A file that was never written comes
// back as the error os.ReadFile gives, which satisfies errors.Is(err,
// fs.ErrNotExist) — the caller has to tell "the session reported a failure"
// from "the session reported nothing at all".
func (v *Vault) ReadRunFile(_ context.Context, storyID, name string) (string, error) {
	if err := safeName("story", storyID); err != nil {
		return "", err
	}
	if err := safeName("run file", name); err != nil {
		return "", err
	}
	written, err := os.ReadFile(v.RunFile(storyID, name))
	if err != nil {
		return "", err
	}
	return string(written), nil
}

// LedgerPath is where a seat's ledger lives in the vault.
func (v *Vault) LedgerPath(seat string) string {
	return filepath.Join(v.dir, SeatsDir, seat, application.LedgerFileName)
}

// AppendToLedger implements application.Vault. The file is opened for append
// and never read back, which is what makes a ledger append-only in practice and
// not merely by convention: there is no code path here that can rewrite a line
// somebody already wrote. Both hosts append to the same file, and the vault's
// .gitattributes tells git to merge it by keeping every line.
func (v *Vault) AppendToLedger(_ context.Context, seat, line string) error {
	if err := safeName("seat", seat); err != nil {
		return err
	}
	if strings.ContainsAny(line, "\r\n") {
		return fmt.Errorf("a ledger line is one line: this one has a newline in it")
	}
	path := v.LedgerPath(seat)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making the %s seat's directory: %w", seat, err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening the %s seat's ledger to append to it: %w", seat, err)
	}
	defer file.Close()

	// A ledger whose last line has no newline of its own would otherwise have
	// this one run onto the end of it. Appending a newline first is the only
	// write this package ever makes that is not the line itself.
	if ends, err := endsInNewline(path); err == nil && !ends {
		if _, err := file.WriteString("\n"); err != nil {
			return fmt.Errorf("appending to the %s seat's ledger: %w", seat, err)
		}
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("appending to the %s seat's ledger: %w", seat, err)
	}
	return nil
}

// ReadLedger implements application.Vault: every line of a seat's ledger, in
// the order it holds them. This is the one place the vault reads a ledger back
// rather than only appending to it — for a report, never to rewrite anything —
// and a seat with no ledger yet reads back as no lines rather than an error.
func (v *Vault) ReadLedger(_ context.Context, seat string) ([]string, error) {
	if err := safeName("seat", seat); err != nil {
		return nil, err
	}
	held, err := os.ReadFile(v.LedgerPath(seat))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the %s seat's ledger: %w", seat, err)
	}
	trimmed := strings.TrimRight(string(held), "\n")
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// endsInNewline reports whether a file's last byte is a newline. An empty file,
// and a file that is not there at all, count as ending in one: there is nothing
// for a new line to run onto.
func endsInNewline(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return true, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return true, err
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		return true, err
	}
	return last[0] == '\n', nil
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

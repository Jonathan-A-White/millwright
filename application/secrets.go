package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// SecretStore is where the factory's tokens are kept: in the vault, encrypted
// to the home's age key, so that no value is ever in the clear in git. A value
// passes through it in memory only; it never names one in an error.
type SecretStore interface {
	// Put sets name to value, adding it or replacing what it held.
	Put(ctx context.Context, name string, value []byte) error
	// Get is the value kept under name: an error wrapping ErrNoSecret when
	// none is.
	Get(ctx context.Context, name string) ([]byte, error)
	// Names is every name a value is kept under, sorted; none, and no error,
	// when nothing is kept yet.
	Names(ctx context.Context) ([]string, error)
}

// ErrNoSecret is what SecretStore.Get wraps when no value is kept by the name
// asked for.
var ErrNoSecret = errors.New("no such secret")

// MaxSecretBytes is the most a value put may be. A token is short; anything
// longer is most likely the wrong thing piped in.
const MaxSecretBytes = 64 << 10

// secretName is what a secret may be called: a word of letters, digits and
// underscores that starts with a letter, as an environment variable is.
var secretName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// unencryptedSuffix is the suffix sops leaves a key's value in the clear for,
// unless its config says otherwise; a secret's name may never end in it.
const unencryptedSuffix = "_unencrypted"

// CheckSecretName refuses a name a secret may not have: anything but a word
// of letters, digits and underscores starting with a letter (64 at most),
// "sops", which is the file's own metadata, and a name ending in
// "_unencrypted", whose value sops would keep in the clear.
func CheckSecretName(name string) error {
	if !secretName.MatchString(name) {
		return fmt.Errorf("the secret name %q is refused: a name is letters, digits and underscores, starting with a letter, 64 at most", name)
	}
	if strings.EqualFold(name, "sops") {
		return fmt.Errorf("the secret name %q is refused: it is the encrypted file's own metadata", name)
	}
	if strings.HasSuffix(strings.ToLower(name), unencryptedSuffix) {
		return fmt.Errorf("the secret name %q is refused: sops keeps a value whose name ends in %s in the clear", name, unencryptedSuffix)
	}
	return nil
}

// SecretsPut is mw secrets put: it reads a value from In, never from the
// command line, where any process listing would show it, and keeps it under a
// name. One trailing newline is dropped, so `echo` and a hand typed line both
// put what was meant. An empty value is refused.
type SecretsPut struct {
	Store SecretStore
	In    io.Reader
	// Out is told the name put, never the value.
	Out io.Writer
	// Vault, when set, commits the secrets file in the vault after the put,
	// that path alone, so the next mw sync carries it and finds nothing of
	// its own uncommitted. Nil leaves the commit to a person.
	Vault SecretsVault
}

// SecretsVault commits paths in the vault; VaultFiles' Commit, narrowed.
type SecretsVault interface {
	Commit(ctx context.Context, message string, paths []string) ([]string, error)
}

// SecretsFile is the vault file the factory's tokens are kept in, encrypted.
const SecretsFile = "secrets.enc.yaml"

// Run implements mw secrets put.
func (p SecretsPut) Run(ctx context.Context, name string) error {
	if err := CheckSecretName(name); err != nil {
		return err
	}
	value, err := io.ReadAll(io.LimitReader(p.In, MaxSecretBytes+1))
	if err != nil {
		return fmt.Errorf("reading the value from stdin: %w", err)
	}
	if len(value) > MaxSecretBytes {
		return fmt.Errorf("the value on stdin is over %d bytes: refused", MaxSecretBytes)
	}
	value = bytes.TrimSuffix(value, []byte("\n"))
	value = bytes.TrimSuffix(value, []byte("\r"))
	if len(value) == 0 {
		return errors.New("the value on stdin is empty: refused (pipe the value in: mw secrets put <name> < file)")
	}
	if !utf8.Valid(value) {
		return errors.New("the value on stdin is not text (UTF-8): refused")
	}
	if err := p.Store.Put(ctx, name, value); err != nil {
		return err
	}
	if p.Vault != nil {
		if _, err := p.Vault.Commit(ctx, "mw secrets put "+name, []string{SecretsFile}); err != nil {
			return fmt.Errorf("%s was put but %s is not committed: %w", name, SecretsFile, err)
		}
	}
	if p.Out != nil {
		fmt.Fprintf(p.Out, "put %s in the vault's %s\n", name, SecretsFile)
	}
	return nil
}

// SecretsGet is mw secrets get: it writes the value kept under a name to Out,
// exactly, with no newline added, so it can be piped into whatever needs it.
// It refuses when Out is a terminal, before anything is decrypted: a value on
// a screen is a value in a scrollback, a screenshot or a transcript.
type SecretsGet struct {
	Store         SecretStore
	Out           io.Writer
	OutIsTerminal bool
}

// Run implements mw secrets get.
func (g SecretsGet) Run(ctx context.Context, name string) error {
	if err := CheckSecretName(name); err != nil {
		return err
	}
	if g.OutIsTerminal {
		return errors.New("stdout is a terminal: mw secrets get writes a value only into a pipe or a file (mw secrets get <name> | <command>)")
	}
	value, err := g.Store.Get(ctx, name)
	if errors.Is(err, ErrNoSecret) {
		return fmt.Errorf("no secret named %s is kept (mw secrets list names them)", name)
	}
	if err != nil {
		return err
	}
	_, err = g.Out.Write(value)
	return err
}

// SecretsList is mw secrets list: the name of every secret kept, one a line,
// sorted, and never a value.
type SecretsList struct {
	Store SecretStore
	Out   io.Writer
}

// Run implements mw secrets list.
func (l SecretsList) Run(ctx context.Context) error {
	names, err := l.Store.Names(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		fmt.Fprintln(l.Out, name)
	}
	return nil
}

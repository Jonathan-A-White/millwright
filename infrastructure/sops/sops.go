// Package sops keeps the factory's tokens in the vault's secrets.enc.yaml,
// encrypted by the sops program to the age recipient the vault's .sops.yaml
// names, and opened with the age key the home alone holds.
//
// A value only ever passes between mw and sops on a pipe: on sops' stdin when
// it is put, on its stdout when it is read. It is never an argument, so no
// process listing shows it, never a temp file, and never in an error: what
// sops says on stderr is passed on with the value cut out of it.
package sops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Program is the sops binary run, found on PATH;
// contrib/install-sops-age.sh installs the pinned release.
const Program = "sops"

// ConfigFile is the vault file naming the age recipient secrets are encrypted
// to; SecretsFile is the encrypted file itself.
const (
	ConfigFile  = ".sops.yaml"
	SecretsFile = application.SecretsFile
)

// metadataKey is the key sops keeps its own metadata under, beside the values.
const metadataKey = "sops"

var _ application.SecretStore = (*Store)(nil)

// Store is application.SecretStore over the sops program.
type Store struct {
	// Vault is the vault directory: SecretsFile and ConfigFile are in it.
	Vault string
	// KeyFile is the age identity that opens the file: ~/.config/mw/age.key
	// on the home. It is the only identity sops is shown.
	KeyFile string
	// Program is the sops binary; Program when empty.
	Program string
}

// New is the store over the vault at vault, opened with the age key at keyFile.
func New(vault, keyFile string) *Store {
	return &Store{Vault: vault, KeyFile: keyFile}
}

func (s *Store) file() string   { return filepath.Join(s.Vault, SecretsFile) }
func (s *Store) config() string { return filepath.Join(s.Vault, ConfigFile) }

// Put implements application.SecretStore. The first value makes the file:
// sops encrypts a document of that one value, read on its stdin, to the
// recipient in ConfigFile, and the ciphertext is written beside it and moved
// into place. After that sops sets the one key in place, the value again on
// its stdin, leaving every other value's ciphertext as it was.
func (s *Store) Put(ctx context.Context, name string, value []byte) error {
	if _, err := os.Stat(s.config()); err != nil {
		return fmt.Errorf("the vault has no %s naming the age recipient to encrypt to (%v): docs/secrets.md says how to make one", ConfigFile, err)
	}
	// A Go string always encodes, so neither Marshal can fail.
	encoded, _ := json.Marshal(string(value))
	index := `["` + name + `"]`

	if _, err := os.Stat(s.file()); err == nil {
		if err := s.needKey(); err != nil {
			return err
		}
		_, err := s.run(ctx, encoded, value, "set", "--value-stdin", s.file(), index)
		if err != nil {
			return fmt.Errorf("sops could not set %s in %s: %w", name, s.file(), err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	document, _ := json.Marshal(map[string]string{name: string(value)})
	ciphertext, err := s.run(ctx, document, value, "encrypt",
		"--input-type", "json", "--output-type", "yaml", "--filename-override", s.file())
	if err != nil {
		return fmt.Errorf("sops could not encrypt %s to the recipient in %s: %w", name, s.config(), err)
	}
	return writeNew(s.file(), ciphertext)
}

// Get implements application.SecretStore.
func (s *Store) Get(ctx context.Context, name string) ([]byte, error) {
	values, err := s.decrypt(ctx)
	if err != nil {
		return nil, err
	}
	raw, ok := values[name]
	if !ok || name == metadataKey {
		return nil, fmt.Errorf("%s: %w", name, application.ErrNoSecret)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("the secret %s in %s is not a string", name, s.file())
	}
	return []byte(value), nil
}

// Names implements application.SecretStore.
func (s *Store) Names(ctx context.Context) ([]string, error) {
	values, err := s.decrypt(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(values))
	for name := range values {
		if name != metadataKey {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// decrypt is every value in the file, still JSON-encoded, held in memory only;
// none when there is no file yet.
func (s *Store) decrypt(ctx context.Context) (map[string]json.RawMessage, error) {
	if _, err := os.Stat(s.file()); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err := s.needKey(); err != nil {
		return nil, err
	}
	plain, err := s.run(ctx, nil, nil, "decrypt", "--output-type", "json", s.file())
	if err != nil {
		return nil, fmt.Errorf("sops could not decrypt %s: %w", s.file(), err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(plain, &values); err != nil {
		return nil, fmt.Errorf("sops decrypted %s to something that is not a map of names", s.file())
	}
	return values, nil
}

// needKey refuses before sops is run when there is no age key here to open the
// file with: a host that is not home has none, and should not.
func (s *Store) needKey() error {
	if _, err := os.Stat(s.KeyFile); err != nil {
		return fmt.Errorf("there is no age key at %s to open %s with: the key is kept on the home only (docs/secrets.md)", s.KeyFile, s.file())
	}
	return nil
}

// run runs sops in the vault with stdin on its stdin and gives what it wrote
// on stdout. Its environment is this process's without any SOPS_ setting, so
// sops is shown the one age identity KeyFile and nothing else, and finds
// ConfigFile in the vault. On failure the error carries what sops said on
// stderr, with value cut out of it wherever it appears, as it is and as JSON
// writes it.
func (s *Store) run(ctx context.Context, stdin, value []byte, args ...string) ([]byte, error) {
	program := s.Program
	if program == "" {
		program = Program
	}
	cmd := exec.CommandContext(ctx, program, append([]string{"--config", s.config()}, args...)...)
	cmd.Dir = s.Vault
	cmd.Env = append(withoutSops(os.Environ()), "SOPS_AGE_KEY_FILE="+s.KeyFile)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(stderr.String())
		if len(value) > 0 {
			quoted, _ := json.Marshal(string(value))
			for _, form := range []string{string(value), string(quoted[1 : len(quoted)-1])} {
				said = strings.ReplaceAll(said, form, "<value>")
			}
		}
		if said == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, said)
	}
	return stdout.Bytes(), nil
}

// withoutSops is env with every SOPS_ variable taken out: a key or a key
// command set in the caller's environment is never what opens the vault.
func withoutSops(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "SOPS_") {
			kept = append(kept, kv)
		}
	}
	return kept
}

// writeNew writes data, the ciphertext, to a temp file beside path and moves
// it into place, so a reader never finds half a file.
func writeNew(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

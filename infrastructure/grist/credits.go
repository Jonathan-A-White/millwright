package grist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// CreditsFile is the spent mark inside the state directory.
const CreditsFile = "credits.json"

// CreditState keeps the spent mark (application's GristCreditState) in
// Dir/credits.json: {"spent_at": "..."} while the key is spent, no file
// otherwise.
type CreditState struct {
	Dir string
}

// CreditState satisfies the port.
var _ application.GristCreditState = (*CreditState)(nil)

// Mark implements application.GristCreditState. A file that says nothing, or
// says {}, is no mark.
func (s *CreditState) Mark(context.Context) (application.GristCreditMark, bool, error) {
	path := filepath.Join(s.Dir, CreditsFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return application.GristCreditMark{}, false, nil
	}
	if err != nil {
		return application.GristCreditMark{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return application.GristCreditMark{}, false, nil
	}
	var mark application.GristCreditMark
	if err := json.Unmarshal(raw, &mark); err != nil {
		return application.GristCreditMark{}, false, fmt.Errorf("%s is not the mill's credits state: %w", path, err)
	}
	return mark, !mark.SpentAt.IsZero(), nil
}

// Spend implements application.GristCreditState: written whole, then renamed
// into place, 0600.
func (s *CreditState) Spend(_ context.Context, mark application.GristCreditMark) error {
	text, err := json.Marshal(mark)
	if err != nil {
		return err
	}
	return New(s.Dir).writeWhole(filepath.Join(s.Dir, CreditsFile), append(text, '\n'))
}

// Clear implements application.GristCreditState.
func (s *CreditState) Clear(context.Context) error {
	err := os.Remove(filepath.Join(s.Dir, CreditsFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// CreditKeyFile reads the Anthropic API key from Path (application's
// GristCreditKey). The file must be a regular file of the user's alone: mode
// 0600, or the key is refused with a plain note and never read.
type CreditKeyFile struct {
	Path string
}

// CreditKeyFile satisfies the port.
var _ application.GristCreditKeyStore = CreditKeyFile{}

// Read implements application.GristCreditKey.
func (k CreditKeyFile) Read(context.Context) (key, refused string) {
	file, err := os.Open(k.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Sprintf("there is no API key file at %s, so grists are billed to the subscription", k.Path)
	}
	if err != nil {
		return "", fmt.Sprintf("the API key file %s cannot be opened (%v), so grists are billed to the subscription", k.Path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Sprintf("the API key file %s is not a regular file, so grists are billed to the subscription", k.Path)
	}
	if info.Mode().Perm() != 0o600 {
		return "", fmt.Sprintf("the API key file %s has mode %04o, not 0600: it is not used, so grists are billed to the subscription (chmod 600 it)", k.Path, info.Mode().Perm())
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4096))
	if err != nil {
		return "", fmt.Sprintf("the API key file %s cannot be read (%v), so grists are billed to the subscription", k.Path, err)
	}
	key = strings.TrimSpace(string(raw))
	if key == "" || strings.ContainsAny(key, " \t\r\n\x00") {
		return "", fmt.Sprintf("the API key file %s does not hold one key, so grists are billed to the subscription", k.Path)
	}
	return key, ""
}

// Present implements application.GristCreditKeyStore: a file of any kind at
// Path, none of it read.
func (k CreditKeyFile) Present(context.Context) (bool, error) {
	_, err := os.Lstat(k.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Write implements application.GristCreditKeyStore: the key and a newline in a
// temporary file of the key file's directory, 0600 from its creation, synced
// and renamed over Path, so a reader sees the old key or the new, whole, and
// a file that was readable by others is replaced by one that is not.
func (k CreditKeyFile) Write(_ context.Context, key string) error {
	dir := filepath.Dir(k.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".anthropic-api-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.WriteString(key + "\n"); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), k.Path)
}

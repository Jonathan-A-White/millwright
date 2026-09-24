// Package postern adapts the Mayor's postern key to disk: a testnet
// secp256k1 key, WIF-encoded, kept in a plain file host-local outside the
// vault and its backups.
package postern

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.PosternKeyFile = (*KeyFile)(nil)

// KeyFile is the postern key file on this host, at path.
type KeyFile struct {
	path string
}

// New is the postern key file at path.
func New(path string) *KeyFile {
	return &KeyFile{path: path}
}

// Path reports where the key file is.
func (k *KeyFile) Path() string { return k.path }

// Exists reports whether the key file is already there.
func (k *KeyFile) Exists() (bool, error) {
	_, err := os.Stat(k.path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("checking for the postern key at %s: %w", k.path, err)
}

// Generate makes a fresh testnet secp256k1 key and writes its WIF encoding to
// the key file, 0600, making its directory first if it is not there.
func (k *KeyFile) Generate() error {
	priv, err := ec.NewPrivateKey()
	if err != nil {
		return fmt.Errorf("generating a postern key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return fmt.Errorf("making the directory for the postern key at %s: %w", k.path, err)
	}
	wif := priv.WifPrefix(byte(ec.TestNet))
	if err := os.WriteFile(k.path, []byte(wif+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing the postern key to %s: %w", k.path, err)
	}
	return nil
}

// PublicKey reads the key file and reports its compressed public key, as
// hex, and its testnet address. It never returns the private key.
func (k *KeyFile) PublicKey() (string, string, error) {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return "", "", fmt.Errorf("reading the postern key at %s: %w", k.path, err)
	}
	priv, err := ec.PrivateKeyFromWif(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", "", fmt.Errorf("the postern key at %s is not a valid WIF key: %w", k.path, err)
	}
	pub := priv.PubKey()
	addr, err := script.NewAddressFromPublicKey(pub, false)
	if err != nil {
		return "", "", fmt.Errorf("deriving the testnet address for the postern key at %s: %w", k.path, err)
	}
	return hex.EncodeToString(pub.Compressed()), addr.AddressString, nil
}

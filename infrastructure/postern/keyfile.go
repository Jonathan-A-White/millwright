// Package postern adapts the postern (github.com/Jonathan-A-White/postern)
// to mw: the Mayor's key on disk, a testnet secp256k1 key, WIF-encoded, kept
// in a plain file host-local outside the vault and its backups (KeyFile); the
// backend's HTTP API (HTTP); BRC-78 encryption (Cipher); and the record
// script and transaction built as postern's own TypeScript builds them.
package postern

import (
	"crypto/sha256"
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
	priv, err := k.privateKey()
	if err != nil {
		return "", "", err
	}
	pub := priv.PubKey()
	addr, err := script.NewAddressFromPublicKey(pub, false)
	if err != nil {
		return "", "", fmt.Errorf("deriving the testnet address for the postern key at %s: %w", k.path, err)
	}
	return hex.EncodeToString(pub.Compressed()), addr.AddressString, nil
}

// PrivateKeyWIF reads the key file and reports its private key, WIF-encoded,
// for Inbox to decrypt a message addressed to it.
func (k *KeyFile) PrivateKeyWIF() (string, error) {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return "", fmt.Errorf("reading the postern key at %s: %w", k.path, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// Sign builds a record transaction, postern's docs/protocol.md section 4
// (buildRecordTransaction), and reports it signed, as raw transaction hex
// ready to broadcast.
func (k *KeyFile) Sign(utxos []application.PosternUtxo, payload []byte) (string, error) {
	priv, err := k.privateKey()
	if err != nil {
		return "", err
	}
	tx, err := buildRecordTransaction(priv, utxos, payload)
	if err != nil {
		return "", err
	}
	return tx.Hex(), nil
}

// SignNonce signs nonce with the postern key, for the Authorization header
// postern's docs/api.md Authentication section describes: a DER-encoded
// ECDSA signature over sha256(nonce) (the nonce string's UTF-8 bytes, a
// single hash, not double), matching @bsv/sdk's PrivateKey.sign(nonceString)
// and Signature.toDER().
func (k *KeyFile) SignNonce(nonce string) (string, error) {
	priv, err := k.privateKey()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(nonce))
	sig, err := priv.Sign(hash[:])
	if err != nil {
		return "", fmt.Errorf("signing the postern backend's challenge: %w", err)
	}
	der, err := sig.ToDER()
	if err != nil {
		return "", fmt.Errorf("DER-encoding the postern challenge signature: %w", err)
	}
	return hex.EncodeToString(der), nil
}

// privateKey reads the key file and parses its private key.
func (k *KeyFile) privateKey() (*ec.PrivateKey, error) {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return nil, fmt.Errorf("reading the postern key at %s: %w", k.path, err)
	}
	priv, err := ec.PrivateKeyFromWif(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("the postern key at %s is not a valid WIF key: %w", k.path, err)
	}
	return priv, nil
}

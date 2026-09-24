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
	"github.com/bsv-blockchain/go-sdk/transaction"
	feemodel "github.com/bsv-blockchain/go-sdk/transaction/fee_model"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"

	"github.com/Jonathan-A-White/millwright/application"
)

// recordProtocolID is the push every postern record script carries, the same
// one the nftgate framing uses (docs/research/messages.md,
// github.com/Jonathan-A-White/postern): `OP_FALSE OP_RETURN <protocol id>
// <version> <payload>`.
const recordProtocolID = "nftgate"

// recordVersion is version 1 of the nftgate payload: plaintext/JSON, the one
// a message's payload travels as. Version 2 is the License-contract format
// and is none of postern's business.
const recordVersion = 0x01

// recordFeeRateSatPerKB is the fee rate a record transaction is built at: the
// testnet floor miners were observed actually accepting
// (docs/research/messages.md).
const recordFeeRateSatPerKB = 1

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

// Sign builds a record transaction — utxos spent as its inputs, one output
// carrying payload as the postern's on-chain record (the nftgate framing,
// version 1), one output paying the change back to this key's own address —
// and reports it signed, as raw transaction hex ready to broadcast.
func (k *KeyFile) Sign(utxos []application.PosternUtxo, payload []byte) (string, error) {
	priv, err := k.privateKey()
	if err != nil {
		return "", err
	}
	addr, err := script.NewAddressFromPublicKey(priv.PubKey(), false)
	if err != nil {
		return "", fmt.Errorf("deriving the testnet address for the postern key at %s: %w", k.path, err)
	}
	lock, err := p2pkh.Lock(addr)
	if err != nil {
		return "", fmt.Errorf("building the postern key's own locking script: %w", err)
	}
	unlocker, err := p2pkh.Unlock(priv, nil)
	if err != nil {
		return "", fmt.Errorf("building the postern key's own unlocking template: %w", err)
	}

	tx := transaction.NewTransaction()
	for _, u := range utxos {
		if err := tx.AddInputFrom(u.Txid, uint32(u.Vout), lock.String(), uint64(u.Satoshis), unlocker); err != nil {
			return "", fmt.Errorf("adding %s:%d as an input: %w", u.Txid, u.Vout, err)
		}
	}
	if err := tx.AddOpReturnPartsOutput([][]byte{[]byte(recordProtocolID), {recordVersion}, payload}); err != nil {
		return "", fmt.Errorf("building the record's output: %w", err)
	}
	tx.AddOutput(&transaction.TransactionOutput{LockingScript: lock, Change: true})
	if err := tx.Fee(&feemodel.SatoshisPerKilobyte{Satoshis: recordFeeRateSatPerKB}, transaction.ChangeDistributionEqual); err != nil {
		return "", fmt.Errorf("computing the record transaction's fee: %w", err)
	}
	if err := tx.Sign(); err != nil {
		return "", fmt.Errorf("signing the record transaction: %w", err)
	}
	return tx.Hex(), nil
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

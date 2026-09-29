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
	"sort"
	"strconv"
	"strings"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.PosternKeyFile = (*KeyFile)(nil)

// spentMemory is how long a send remembers the outputs it spent: a block
// (about ten minutes on testnet) surely confirms the spend within it, after
// which the block explorer stops listing them.
const spentMemory = 2 * time.Hour

// KeyFile is the postern key file on this host, at path.
type KeyFile struct {
	path string
	now  func() time.Time
}

// New is the postern key file at path.
func New(path string) *KeyFile {
	return &KeyFile{path: path, now: time.Now}
}

// WithClock reads the time from now, to remember spent outputs against.
func (k *KeyFile) WithClock(now func() time.Time) *KeyFile {
	k.now = now
	return k
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
// ready to broadcast. It leaves out the outputs MarkSpent remembers a send
// spent in the last two hours, which the block explorer may still list, and
// counts among the coins to spend the change MarkSent remembers, which the
// block explorer may not list until a block confirms it. It spends a
// confirmed coin, the largest first, before an unconfirmed one, the oldest
// first.
func (k *KeyFile) Sign(utxos []application.PosternUtxo, payload []byte) (string, error) {
	priv, err := k.privateKey()
	if err != nil {
		return "", err
	}
	spent, err := k.readSpent()
	if err != nil {
		return "", err
	}
	own, err := k.readOwnChange()
	if err != nil {
		return "", err
	}
	age := map[string]int64{}
	listed := map[string]bool{}
	for _, u := range utxos {
		listed[outpoint(u)] = true
	}
	candidates := make([]application.PosternUtxo, 0, len(utxos)+len(own))
	for _, u := range utxos {
		if _, ok := spent[outpoint(u)]; !ok {
			candidates = append(candidates, u)
		}
	}
	for point, coin := range own {
		if _, ok := spent[point]; !ok && !listed[point] {
			candidates = append(candidates, coin.utxo)
			age[point] = coin.at
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if (a.Height > 0) != (b.Height > 0) {
			return a.Height > 0
		}
		if a.Height == 0 && age[outpoint(a)] != age[outpoint(b)] {
			return age[outpoint(a)] < age[outpoint(b)]
		}
		if a.Satoshis != b.Satoshis {
			return a.Satoshis > b.Satoshis
		}
		return outpoint(a) < outpoint(b)
	})
	tx, err := buildRecordTransaction(priv, candidates, payload)
	if err != nil {
		return "", err
	}
	return tx.Hex(), nil
}

// ownChange is a change output a send of this key made: the coin, and when
// (unix seconds) it was made.
type ownChange struct {
	utxo application.PosternUtxo
	at   int64
}

// ownChangePath is the file that remembers the change this key's sends made,
// beside the key file like spentPath.
func (k *KeyFile) ownChangePath() string { return k.path + ".change" }

// readOwnChange reads the change outputs a send made in the last two hours,
// by outpoint. No file is none.
func (k *KeyFile) readOwnChange() (map[string]ownChange, error) {
	own := map[string]ownChange{}
	raw, err := os.ReadFile(k.ownChangePath())
	if os.IsNotExist(err) {
		return own, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the change this key made at %s: %w", k.ownChangePath(), err)
	}
	cutoff := k.now().Add(-spentMemory).Unix()
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		at, aerr := strconv.ParseInt(fields[0], 10, 64)
		sats, serr := strconv.ParseInt(fields[2], 10, 64)
		txid, vout, ok := strings.Cut(fields[1], ":")
		n, verr := strconv.Atoi(vout)
		if !ok || aerr != nil || serr != nil || verr != nil || at <= cutoff {
			continue
		}
		own[fields[1]] = ownChange{utxo: application.PosternUtxo{Txid: txid, Vout: n, Satoshis: sats}, at: at}
	}
	return own, nil
}

// MarkSent remembers what a send just broadcast as rawtx: the outputs it
// spent (as MarkSpent does), and the change it paid back to this key, which
// a later send may spend while it is unconfirmed.
func (k *KeyFile) MarkSent(rawtx string) error {
	priv, err := k.privateKey()
	if err != nil {
		return err
	}
	tx, err := transaction.NewTransactionFromHex(rawtx)
	if err != nil {
		return fmt.Errorf("reading the broadcast transaction to remember its change: %w", err)
	}
	var spent []application.PosternUtxo
	for _, in := range tx.Inputs {
		spent = append(spent, application.PosternUtxo{Txid: in.SourceTXID.String(), Vout: int(in.SourceTxOutIndex)})
	}
	if err := k.MarkSpent(spent); err != nil {
		return err
	}
	own, err := script.NewAddressFromPublicKey(priv.PubKey(), false)
	if err != nil {
		return fmt.Errorf("deriving the postern key's testnet address: %w", err)
	}
	ownLock, err := p2pkh.Lock(own)
	if err != nil {
		return fmt.Errorf("building the postern key's own locking script: %w", err)
	}
	coins, err := k.readOwnChange()
	if err != nil {
		return err
	}
	now := k.now().Unix()
	for vout, o := range tx.Outputs {
		if vout >= 2 && o.LockingScript.Equals(ownLock) && o.Satoshis > 1 {
			coins[fmt.Sprintf("%s:%d", tx.TxID(), vout)] = ownChange{
				utxo: application.PosternUtxo{Txid: tx.TxID().String(), Vout: vout, Satoshis: int64(o.Satoshis)}, at: now,
			}
		}
	}
	var out strings.Builder
	for point, c := range coins {
		fmt.Fprintf(&out, "%d %s %d\n", c.at, point, c.utxo.Satoshis)
	}
	if err := os.WriteFile(k.ownChangePath(), []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("writing the change this key made to %s: %w", k.ownChangePath(), err)
	}
	return nil
}

// spentPath is the file that remembers what this key's sends spent: beside
// the key file, so it is host-local and outside the vault and its backups too.
func (k *KeyFile) spentPath() string { return k.path + ".spent" }

// readSpent reads the outpoints a send spent in the last two hours, each
// with when it was spent (unix seconds). No file is none spent.
func (k *KeyFile) readSpent() (map[string]int64, error) {
	spent := map[string]int64{}
	raw, err := os.ReadFile(k.spentPath())
	if os.IsNotExist(err) {
		return spent, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the outputs this key spent at %s: %w", k.spentPath(), err)
	}
	cutoff := k.now().Add(-spentMemory).Unix()
	for _, line := range strings.Split(string(raw), "\n") {
		at, point, ok := strings.Cut(strings.TrimSpace(line), " ")
		when, perr := strconv.ParseInt(at, 10, 64)
		if !ok || perr != nil || when <= cutoff {
			continue
		}
		spent[point] = when
	}
	return spent, nil
}

// MarkSpent remembers that a send just spent utxos (but a 1-satoshi one,
// which Sign never spends), so that Sign leaves them out for the next two
// hours, while the block explorer may still list them. It forgets the ones
// older than that.
func (k *KeyFile) MarkSpent(utxos []application.PosternUtxo) error {
	spent, err := k.readSpent()
	if err != nil {
		return err
	}
	now := k.now().Unix()
	for _, u := range utxos {
		if u.Satoshis != 1 {
			spent[outpoint(u)] = now
		}
	}
	var out strings.Builder
	for point, when := range spent {
		fmt.Fprintf(&out, "%d %s\n", when, point)
	}
	if err := os.MkdirAll(filepath.Dir(k.spentPath()), 0o700); err != nil {
		return fmt.Errorf("making the directory for the outputs this key spent at %s: %w", k.spentPath(), err)
	}
	if err := os.WriteFile(k.spentPath(), []byte(out.String()), 0o600); err != nil {
		return fmt.Errorf("writing the outputs this key spent to %s: %w", k.spentPath(), err)
	}
	return nil
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

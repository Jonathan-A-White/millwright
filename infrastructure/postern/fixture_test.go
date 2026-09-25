package postern_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bsv-blockchain/go-sdk/script"
)

// fixtureRandomnessSeed is scripts/generate-fixture.ts's seed for
// deterministicRandomValues, the stand-in for Web Crypto's
// getRandomValues that pins encryptMessage's keyID and AES-GCM IV so the
// committed fixture never changes between runs.
const fixtureRandomnessSeed = "postern-protocol-fixture-v1"

// fixtureRandomness is an infinite byte stream, sha256(seed:0) ||
// sha256(seed:1) || ..., reproducing deterministicRandomValues' output
// byte for byte. go-sdk's message.Encrypt and SymmetricKey.Encrypt each
// draw exactly 32 bytes from crypto/rand, in the same order @bsv/sdk's
// EncryptedMessage.encrypt draws its 32-byte keyID and 32-byte IV, so
// reading this stream through crypto/rand.Reader reproduces the same
// ciphertext the fixture's generator wrote.
type fixtureRandomness struct {
	counter int
	buf     []byte
}

func (f *fixtureRandomness) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if len(f.buf) == 0 {
			digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", fixtureRandomnessSeed, f.counter)))
			f.counter++
			f.buf = digest[:]
		}
		copied := copy(p[n:], f.buf)
		f.buf = f.buf[copied:]
		n += copied
	}
	return n, nil
}

// pinFixtureRandomness swaps crypto/rand.Reader for fixtureRandomness for
// the life of the calling test, restoring the real one on cleanup.
func pinFixtureRandomness(t *testing.T) {
	t.Helper()
	real := rand.Reader
	rand.Reader = &fixtureRandomness{}
	t.Cleanup(func() { rand.Reader = real })
}

// protocolFixture is testdata/protocol-vectors.json, postern's own
// machine-checkable protocol fixture (docs/protocol.md section 5,
// scripts/generate-fixture.ts): fixed sender/recipient keys, a fixed
// plaintext and a fixed fake UTXO, encryptMessage's output, the record
// script it travels in, and the fully signed send transaction built from it.
type protocolFixture struct {
	Inputs struct {
		SenderPrivateKeyHex    string `json:"senderPrivateKeyHex"`
		SenderPublicKeyHex     string `json:"senderPublicKeyHex"`
		RecipientPrivateKeyHex string `json:"recipientPrivateKeyHex"`
		RecipientPublicKeyHex  string `json:"recipientPublicKeyHex"`
		Plaintext              string `json:"plaintext"`
		MessageClass           string `json:"messageClass"`
		Ts                     int64  `json:"ts"`
		Utxo                   struct {
			Txid     string `json:"txid"`
			Vout     int    `json:"vout"`
			Satoshis int64  `json:"satoshis"`
			Script   string `json:"script"`
		} `json:"utxo"`
	} `json:"inputs"`
	EncryptMessage struct {
		V     int    `json:"v"`
		Kind  string `json:"kind"`
		Class string `json:"class"`
		To    string `json:"to"`
		From  string `json:"from"`
		Ts    int64  `json:"ts"`
		Ct    string `json:"ct"`
	} `json:"encryptMessage"`
	RecordScriptHex string `json:"recordScriptHex"`
	Transaction     struct {
		RawtxHex string `json:"rawtxHex"`
		Txid     string `json:"txid"`
	} `json:"transaction"`
}

func loadProtocolFixture(t *testing.T) protocolFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/protocol-vectors.json")
	if err != nil {
		t.Fatalf("reading the protocol fixture: %v", err)
	}
	var f protocolFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parsing the protocol fixture: %v", err)
	}
	return f
}

// wifFromHex converts a raw secp256k1 private key, hex, to a testnet WIF
// encoding: the fixture carries hex (postern's own TypeScript has no WIF of
// its own), and the postern key file only ever holds WIF.
func wifFromHex(t *testing.T, privKeyHex string) string {
	t.Helper()
	priv, err := ec.PrivateKeyFromHex(privKeyHex)
	if err != nil {
		t.Fatalf("parsing %s as a private key: %v", privKeyHex, err)
	}
	return priv.WifPrefix(byte(ec.TestNet))
}

// addressFromHex is the testnet P2PKH address for a compressed public key,
// hex, the way script.NewAddressFromPublicKey derives one for the postern
// key's own change output.
func addressFromHex(t *testing.T, pubKeyHex string) string {
	t.Helper()
	addr, err := script.NewAddressFromPublicKeyString(pubKeyHex, false)
	if err != nil {
		t.Fatalf("deriving the testnet address for %s: %v", pubKeyHex, err)
	}
	return addr.AddressString
}

// keyFileHolding writes wif to a key file in a temp dir, 0600, the way
// mw postern key init would have.
func keyFileHolding(t *testing.T, wif string) string {
	t.Helper()
	path := t.TempDir() + "/postern.key"
	if err := os.WriteFile(path, []byte(wif+"\n"), 0o600); err != nil {
		t.Fatalf("writing the key file: %v", err)
	}
	return path
}

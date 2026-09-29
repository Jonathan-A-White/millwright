package postern_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// vectorKeyFile is the grist vectors' key by name, written as a key file, and
// its public key as the vectors give it.
func vectorKeyFile(t *testing.T, name string) (*postern.KeyFile, string) {
	t.Helper()
	raw, err := os.ReadFile("../../application/testdata/grist-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Keys map[string]struct {
			PrivateKeyHex string `json:"privateKeyHex"`
			PublicKeyHex  string `json:"publicKeyHex"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	secret, err := hex.DecodeString(v.Keys[name].PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	priv, _ := ec.PrivateKeyFromBytes(secret)
	path := filepath.Join(t.TempDir(), name+".key")
	if err := os.WriteFile(path, []byte(priv.WifPrefix(byte(ec.TestNet))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return postern.New(path), v.Keys[name].PublicKeyHex
}

// A grist sealed by Cairn's phone to the mill key opens with the mill key,
// proving the phone sealed it; the mill's answer, sealed back, opens with the
// phone's. The vectors' keys are the keys a key file holds.
func TestGristSealsBetweenThePhoneAndTheMill(t *testing.T) {
	millKeys, millPub := vectorKeyFile(t, "mill")
	phoneKeys, phonePub := vectorKeyFile(t, "cairnPhone")
	if got, _, _ := millKeys.PublicKey(); got != millPub {
		t.Fatalf("the mill key file's public key is %s, the vectors say %s", got, millPub)
	}

	sealed, err := postern.NewCipher(phoneKeys).Encrypt(millPub, `{"grist":{"app":"cairn","kind":"sweep","v":"1.1"}}`)
	if err != nil {
		t.Fatal(err)
	}
	millWIF, _ := millKeys.PrivateKeyWIF()
	text, from, err := postern.NewCipher(millKeys).Decrypt(millWIF, sealed)
	if err != nil || from != phonePub || text != `{"grist":{"app":"cairn","kind":"sweep","v":"1.1"}}` {
		t.Fatalf("expected the grist opened, from the phone, got %q from %s: %v", text, from, err)
	}

	answer, err := postern.NewCipher(millKeys).Encrypt(phonePub, `{"re":"direct:x","status":"answered"}`)
	if err != nil {
		t.Fatal(err)
	}
	phoneWIF, _ := phoneKeys.PrivateKeyWIF()
	if text, from, err := postern.NewCipher(phoneKeys).Decrypt(phoneWIF, answer); err != nil || from != millPub || text != `{"re":"direct:x","status":"answered"}` {
		t.Fatalf("expected the answer opened by the phone, from the mill, got %q from %s: %v", text, from, err)
	}
}

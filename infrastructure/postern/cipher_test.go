package postern_test

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

func TestCipherDecryptsTheFixturesCiphertext(t *testing.T) {
	f := loadRecordFixture(t)

	text, err := postern.NewCipher(postern.New(keyFileHolding(t, f.SenderWIF))).Decrypt(f.RecipientWIF, f.Ct)
	if err != nil {
		t.Fatalf("decrypting the fixture: %v", err)
	}
	if text != f.Text {
		t.Fatalf("expected %q, got %q", f.Text, text)
	}
}

func TestCipherRoundTripsFromTheKeyFileToTheRecipient(t *testing.T) {
	f := loadRecordFixture(t)
	cipher := postern.NewCipher(postern.New(keyFileHolding(t, f.SenderWIF)))

	ct, err := cipher.Encrypt(f.RecipientPubKey, "Ship it?")
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	text, err := cipher.Decrypt(f.RecipientWIF, ct)
	if err != nil {
		t.Fatalf("decrypting: %v", err)
	}
	if text != "Ship it?" {
		t.Fatalf("expected %q, got %q", "Ship it?", text)
	}

	// BRC-78 carries the sender's and the recipient's public keys in the clear
	// at the head of the ciphertext, after the four version bytes.
	raw, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		t.Fatalf("the ciphertext is not base64: %v", err)
	}
	if got := hex.EncodeToString(raw[:4]); got != "42421033" {
		t.Errorf("expected the BRC-78 version 42421033, got %s", got)
	}
	if got := hex.EncodeToString(raw[4:37]); got != f.SenderPubKey {
		t.Errorf("expected the sender %s, got %s", f.SenderPubKey, got)
	}
	if got := hex.EncodeToString(raw[37:70]); got != f.RecipientPubKey {
		t.Errorf("expected the recipient %s, got %s", f.RecipientPubKey, got)
	}
}

func TestCipherRefusesAKeyTheMessageIsNotFor(t *testing.T) {
	f := loadRecordFixture(t)
	other, err := ec.NewPrivateKey()
	if err != nil {
		t.Fatalf("making a key: %v", err)
	}

	_, err = postern.NewCipher(postern.New(keyFileHolding(t, f.SenderWIF))).Decrypt(other.WifPrefix(byte(ec.TestNet)), f.Ct)
	if err == nil {
		t.Fatal("expected a key the message is not for to be refused")
	}
}

func TestCipherRefusesARecipientThatIsNotAPublicKey(t *testing.T) {
	f := loadRecordFixture(t)

	_, err := postern.NewCipher(postern.New(keyFileHolding(t, f.SenderWIF))).Encrypt("governor-pubkey-hex", "hello")
	if err == nil || !strings.Contains(err.Error(), "governor-pubkey-hex") {
		t.Fatalf("expected the refusal to name the bad key, got: %v", err)
	}
}

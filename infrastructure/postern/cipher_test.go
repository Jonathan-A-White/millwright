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
	f := loadProtocolFixture(t)

	text, err := postern.NewCipher(postern.New(keyFileHolding(t, "unused"))).Decrypt(
		wifFromHex(t, f.Inputs.RecipientPrivateKeyHex), f.EncryptMessage.Ct)
	if err != nil {
		t.Fatalf("decrypting the fixture: %v", err)
	}
	if text != f.Inputs.Plaintext {
		t.Fatalf("expected %q, got %q", f.Inputs.Plaintext, text)
	}
}

// With crypto/rand pinned to the same deterministic byte stream postern's
// own generator drew its randomness from (scripts/generate-fixture.ts),
// Cipher.Encrypt's output is byte-identical to the fixture's ciphertext:
// same keyID, same AES-GCM IV, same shared secret, same everything.
func TestCipherEncryptReproducesTheFixturesCiphertextUnderPinnedRandomness(t *testing.T) {
	f := loadProtocolFixture(t)
	senderWIF := wifFromHex(t, f.Inputs.SenderPrivateKeyHex)
	cipher := postern.NewCipher(postern.New(keyFileHolding(t, senderWIF)))

	pinFixtureRandomness(t)
	ct, err := cipher.Encrypt(f.Inputs.RecipientPublicKeyHex, f.Inputs.Plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	if ct != f.EncryptMessage.Ct {
		t.Fatalf("expected the fixture's ciphertext\n%s\ngot\n%s", f.EncryptMessage.Ct, ct)
	}
}

func TestCipherRoundTripsFromTheKeyFileToTheRecipient(t *testing.T) {
	f := loadProtocolFixture(t)
	senderWIF := wifFromHex(t, f.Inputs.SenderPrivateKeyHex)
	cipher := postern.NewCipher(postern.New(keyFileHolding(t, senderWIF)))

	ct, err := cipher.Encrypt(f.Inputs.RecipientPublicKeyHex, "Ship it?")
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	text, err := cipher.Decrypt(wifFromHex(t, f.Inputs.RecipientPrivateKeyHex), ct)
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
	if got := hex.EncodeToString(raw[4:37]); got != f.Inputs.SenderPublicKeyHex {
		t.Errorf("expected the sender %s, got %s", f.Inputs.SenderPublicKeyHex, got)
	}
	if got := hex.EncodeToString(raw[37:70]); got != f.Inputs.RecipientPublicKeyHex {
		t.Errorf("expected the recipient %s, got %s", f.Inputs.RecipientPublicKeyHex, got)
	}
}

func TestCipherRefusesAKeyTheMessageIsNotFor(t *testing.T) {
	f := loadProtocolFixture(t)
	other, err := ec.NewPrivateKey()
	if err != nil {
		t.Fatalf("making a key: %v", err)
	}

	_, err = postern.NewCipher(postern.New(keyFileHolding(t, "unused"))).Decrypt(other.WifPrefix(byte(ec.TestNet)), f.EncryptMessage.Ct)
	if err == nil {
		t.Fatal("expected a key the message is not for to be refused")
	}
}

func TestCipherRefusesARecipientThatIsNotAPublicKey(t *testing.T) {
	f := loadProtocolFixture(t)

	_, err := postern.NewCipher(postern.New(keyFileHolding(t, wifFromHex(t, f.Inputs.SenderPrivateKeyHex)))).Encrypt("governor-pubkey-hex", "hello")
	if err == nil || !strings.Contains(err.Error(), "governor-pubkey-hex") {
		t.Fatalf("expected the refusal to name the bad key, got: %v", err)
	}
}

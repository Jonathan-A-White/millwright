package postern

import (
	"encoding/base64"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/message"
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.Cipher = (*Cipher)(nil)

// Cipher is BRC-78 EncryptedMessage (postern's docs/protocol.md section 2),
// go-sdk's message package, which is the same construction @bsv/sdk's
// EncryptedMessage.encrypt/decrypt implement on postern's side. It encrypts
// from the key in keys, the sending side of every message mw sends.
type Cipher struct {
	keys *KeyFile
}

// NewCipher is BRC-78 with keys as the sender.
func NewCipher(keys *KeyFile) *Cipher {
	return &Cipher{keys: keys}
}

// Encrypt implements application.Cipher: text, UTF-8, encrypted from the key
// file's key to toPubKey, base64.
func (c *Cipher) Encrypt(toPubKey, text string) (string, error) {
	to, err := ec.PublicKeyFromString(toPubKey)
	if err != nil {
		return "", fmt.Errorf("%q is not a compressed public key, hex: %w", toPubKey, err)
	}
	from, err := c.keys.privateKey()
	if err != nil {
		return "", err
	}
	ct, err := message.Encrypt([]byte(text), from, to)
	if err != nil {
		return "", fmt.Errorf("encrypting for %s: %w", toPubKey, err)
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt implements application.Cipher: ciphertext, base64, decrypted with
// privKey, WIF. A ciphertext for another key, or one whose AES-GCM tag does
// not verify, is an error.
func (c *Cipher) Decrypt(privKey, ciphertext string) (string, error) {
	priv, err := ec.PrivateKeyFromWif(privKey)
	if err != nil {
		return "", fmt.Errorf("the key to decrypt with is not a valid WIF key: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("the ciphertext is not base64: %w", err)
	}
	text, err := message.Decrypt(ct, priv)
	if err != nil {
		return "", err
	}
	return string(text), nil
}

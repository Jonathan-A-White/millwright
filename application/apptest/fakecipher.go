package apptest

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeCipher is an in-memory application.Cipher: it does no real
// cryptography, only a reversible encoding, so a test can prove a message
// round-trips without a real key pair. Addressing (whether a privKey may open
// a ciphertext) is the caller's job — PosternInbox filters by a record's To
// before it ever calls Decrypt — so Decrypt here never checks it either.
type FakeCipher struct {
	// From is the sender identity every Encrypt call embeds into its
	// ciphertext, standing in for BRC-78's own sender public key: what
	// Decrypt reports back as envelopeFrom, regardless of what a
	// PosternRecord's own From field is separately set to.
	From string
	// Err, when set, is returned by every method instead of doing the work.
	Err error
}

// FakeCipher satisfies the port.
var _ application.Cipher = (*FakeCipher)(nil)

// NewFakeCipher returns a cipher that encodes rather than encrypts.
func NewFakeCipher() *FakeCipher {
	return &FakeCipher{}
}

// Encrypt implements application.Cipher.
func (f *FakeCipher) Encrypt(toPubKey, text string) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	if strings.TrimSpace(toPubKey) == "" {
		return "", fmt.Errorf("encrypting: no recipient key")
	}
	return base64.StdEncoding.EncodeToString([]byte(toPubKey + "\x00" + f.From + "\x00" + text)), nil
}

// Decrypt implements application.Cipher.
func (f *FakeCipher) Decrypt(privKey, ciphertext string) (string, string, error) {
	if f.Err != nil {
		return "", "", f.Err
	}
	if strings.TrimSpace(privKey) == "" {
		return "", "", fmt.Errorf("decrypting: no private key")
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", "", fmt.Errorf("decrypting: not valid ciphertext: %w", err)
	}
	parts := strings.SplitN(string(raw), "\x00", 3)
	if len(parts) != 3 {
		return "", "", fmt.Errorf("decrypting: malformed ciphertext")
	}
	return parts[2], parts[1], nil
}

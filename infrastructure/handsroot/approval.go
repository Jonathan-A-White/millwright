// Package handsroot is mw-hands-root, the small root-owned program that runs
// a hands step as root only once the Governor has approved it with his key
// (postern's docs/protocol.md §17), and the one check of that approval both
// it and mw make. It imports the domain and the signature library, nothing
// of mw's own beyond: the program built from it runs as root.
package handsroot

import (
	"encoding/hex"
	"fmt"
	"strings"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/domain"
)

// VerifyApproval reports whether sigDERHex is governorKeyHex's signature of
// the step hashing to sha256Hex, approved at approvedAt: a DER-encoded ECDSA
// signature over the SHA-256 of domain.HandsApprovalMessage, as @bsv/sdk's
// PrivateKey.sign makes one. Anything else is an error saying which part
// failed.
func VerifyApproval(governorKeyHex, sha256Hex string, approvedAt int64, sigDERHex string) error {
	key, err := ec.PublicKeyFromString(strings.TrimSpace(governorKeyHex))
	if err != nil {
		return fmt.Errorf("the Governor's key %q is not a public key: %w", governorKeyHex, err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(sigDERHex))
	if err != nil || len(raw) == 0 {
		return fmt.Errorf("the approval's signature is not hex")
	}
	sig, err := ec.ParseDERSignature(raw)
	if err != nil {
		return fmt.Errorf("the approval's signature is not a DER signature: %w", err)
	}
	if !sig.Verify(domain.HandsApprovalDigest(sha256Hex, approvedAt), key) {
		return fmt.Errorf("the approval's signature is not the Governor's over this step at this time")
	}
	return nil
}

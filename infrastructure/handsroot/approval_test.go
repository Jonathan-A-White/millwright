package handsroot_test

import (
	"encoding/hex"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/handsroot"
)

// The "hands" vector of postern's docs/fixtures/protocol-vectors.json: the
// Governor's key and his signature, made by @bsv/sdk's PrivateKey.sign
// (deterministic, RFC 6979) over sha256 of the approval string.
const (
	vectorSHA256     = "2d74974c1cc77dd9faa271dc7c6b18dd690aa369c2cba8c5067b3c56bf42fe14"
	vectorApprovedAt = int64(1790000000)
	vectorGovernor   = "03f01d6b9018ab421dd410404cb869072065522bf85734008f105cf385a023a80f"
	vectorSig        = "3044022044fb7a49fdaeda2ff47ed5d2d70d9371399740dd4dca94c8bcd2efe3ca2a8d4602200d029784332f7d32334d7bf3c8cdcabda8ed381fcbf0799826ec460262c06bb3"
)

func TestVerifyApprovalAcceptsTheTypeScriptVectorsSignature(t *testing.T) {
	if err := handsroot.VerifyApproval(vectorGovernor, vectorSHA256, vectorApprovedAt, vectorSig); err != nil {
		t.Fatalf("expected the vector's signature to verify, got %v", err)
	}
}

// Anything the signature does not cover exactly — another time, another
// step, another key, a mangled signature — is refused.
func TestVerifyApprovalRefusesWhatTheSignatureDoesNotCover(t *testing.T) {
	other, err := ec.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	otherKey := hex.EncodeToString(other.PubKey().Compressed())
	cases := map[string]error{
		"another time":  handsroot.VerifyApproval(vectorGovernor, vectorSHA256, vectorApprovedAt+1, vectorSig),
		"another step":  handsroot.VerifyApproval(vectorGovernor, "0"+vectorSHA256[1:], vectorApprovedAt, vectorSig),
		"another key":   handsroot.VerifyApproval(otherKey, vectorSHA256, vectorApprovedAt, vectorSig),
		"not DER":       handsroot.VerifyApproval(vectorGovernor, vectorSHA256, vectorApprovedAt, "30440220"),
		"not hex":       handsroot.VerifyApproval(vectorGovernor, vectorSHA256, vectorApprovedAt, "zz"),
		"not a key":     handsroot.VerifyApproval("02abc", vectorSHA256, vectorApprovedAt, vectorSig),
		"an empty key":  handsroot.VerifyApproval("", vectorSHA256, vectorApprovedAt, vectorSig),
		"an empty sig ": handsroot.VerifyApproval(vectorGovernor, vectorSHA256, vectorApprovedAt, ""),
	}
	for name, err := range cases {
		if err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

// A signature made the way the app makes one — sha256 of the approval
// string, signed — verifies; this is how the other tests sign.
func TestVerifyApprovalAcceptsASignatureMadeTheAppsWay(t *testing.T) {
	priv, err := ec.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	key := hex.EncodeToString(priv.PubKey().Compressed())
	if err := handsroot.VerifyApproval(key, vectorSHA256, 1790000123, sign(t, priv, vectorSHA256, 1790000123)); err != nil {
		t.Fatalf("expected it to verify, got %v", err)
	}
}

func sign(t *testing.T, priv *ec.PrivateKey, sha string, at int64) string {
	t.Helper()
	sig, err := priv.Sign(domain.HandsApprovalDigest(sha, at))
	if err != nil {
		t.Fatal(err)
	}
	der, err := sig.ToDER()
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(der)
}

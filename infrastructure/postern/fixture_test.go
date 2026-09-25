package postern_test

import (
	"encoding/json"
	"os"
	"testing"
)

// recordFixture is testdata/record.json: one postern message record, the keys
// on both ends of it, and the exact script bytes it travels in on chain.
type recordFixture struct {
	SenderWIF       string `json:"senderWIF"`
	SenderPubKey    string `json:"senderPubKey"`
	SenderAddress   string `json:"senderAddress"`
	RecipientWIF    string `json:"recipientWIF"`
	RecipientPubKey string `json:"recipientPubKey"`
	Text            string `json:"text"`
	Class           string `json:"class"`
	Ts              int64  `json:"ts"`
	Ct              string `json:"ct"`
	Payload         string `json:"payload"`
	ScriptHex       string `json:"scriptHex"`
}

func loadRecordFixture(t *testing.T) recordFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/record.json")
	if err != nil {
		t.Fatalf("reading the record fixture: %v", err)
	}
	var f recordFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parsing the record fixture: %v", err)
	}
	return f
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

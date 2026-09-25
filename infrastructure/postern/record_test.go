package postern_test

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// fixturePayload rebuilds the payload bytes postern's own TypeScript wrote
// (application.PosternPayload's field order matches its JSON key order, the
// same reasoning documented on that type), from the fixture's encryptMessage.
func fixturePayload(t *testing.T, f protocolFixture) []byte {
	t.Helper()
	payload, err := json.Marshal(application.PosternPayload{
		V:     f.EncryptMessage.V,
		Kind:  f.EncryptMessage.Kind,
		Class: f.EncryptMessage.Class,
		To:    f.EncryptMessage.To,
		From:  f.EncryptMessage.From,
		Ts:    f.EncryptMessage.Ts,
		Ct:    f.EncryptMessage.Ct,
	})
	if err != nil {
		t.Fatalf("marshaling the fixture's payload: %v", err)
	}
	return payload
}

func TestRecordScriptIsTheFixturesBytes(t *testing.T) {
	f := loadProtocolFixture(t)

	got, err := postern.RecordScript(fixturePayload(t, f))
	if err != nil {
		t.Fatalf("building the record script: %v", err)
	}
	if hex.EncodeToString(*got) != f.RecordScriptHex {
		t.Fatalf("expected the record script\n%s\ngot\n%s", f.RecordScriptHex, hex.EncodeToString(*got))
	}
}

func TestDecodeRecordScriptReadsTheFixturesPayload(t *testing.T) {
	f := loadProtocolFixture(t)

	payload, ok := postern.DecodeRecordScript(f.RecordScriptHex)
	if !ok {
		t.Fatal("expected the fixture's script to decode as a version-1 record")
	}
	if string(payload) != string(fixturePayload(t, f)) {
		t.Fatalf("expected the payload\n%s\ngot\n%s", fixturePayload(t, f), payload)
	}
}

func TestDecodeRecordScriptRefusesWhatIsNotAVersionOneRecord(t *testing.T) {
	for name, scriptHex := range map[string]string{
		"a P2PKH output":         "76a914000000000000000000000000000000000000000088ac",
		"a foreign OP_RETURN":    "006a03666f6f0101027b7d",
		"version 2":              "006a076e66746761746501020101",
		"a missing payload":      "006a076e667467617465" + "0101",
		"a push that runs short": "006a076e667467617465" + "0101" + "05ab",
		"not hex":                "zz",
	} {
		if _, ok := postern.DecodeRecordScript(scriptHex); ok {
			t.Errorf("expected %s not to decode as a record", name)
		}
	}
}

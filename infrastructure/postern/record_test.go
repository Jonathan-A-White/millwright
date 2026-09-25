package postern_test

import (
	"encoding/hex"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

func TestRecordScriptIsTheFixturesBytes(t *testing.T) {
	f := loadRecordFixture(t)

	got, err := postern.RecordScript([]byte(f.Payload))
	if err != nil {
		t.Fatalf("building the record script: %v", err)
	}
	if hex.EncodeToString(*got) != f.ScriptHex {
		t.Fatalf("expected the record script\n%s\ngot\n%s", f.ScriptHex, hex.EncodeToString(*got))
	}
}

func TestDecodeRecordScriptReadsTheFixturesPayload(t *testing.T) {
	f := loadRecordFixture(t)

	payload, ok := postern.DecodeRecordScript(f.ScriptHex)
	if !ok {
		t.Fatal("expected the fixture's script to decode as a version-1 record")
	}
	if string(payload) != f.Payload {
		t.Fatalf("expected the payload\n%s\ngot\n%s", f.Payload, payload)
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

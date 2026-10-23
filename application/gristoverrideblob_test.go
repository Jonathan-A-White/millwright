package application_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const (
	blobFactoryKey = "03aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	blobPhoneKey   = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	posternCap     = 256 * 1024
)

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// A ref's text too big for one Postern message goes as a blob sealed to the
// mill, named by hash, and the mill grinds with it.
func TestARefsTextOverThePosternCapGoesAsABlobAndIsGroundWithIt(t *testing.T) {
	dir := t.TempDir()
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, []byte(`{"schemaVersion":"1","question":"hi"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mill, _, grinder := overrideMill(t, "factory", "eval:greets:ab12cd34")
	millKey, _, _ := (&fakeMillKey{made: true}).PublicKey()

	outbox := apptest.NewFakePostern()
	outbox.Mine = application.PosternMe{Mill: millKey}
	outbox.MaxDeliverBytes = posternCap
	send := application.GristSend{Postern: outbox, Cipher: &apptest.FakeCipher{From: blobFactoryKey}, Keys: &fakeMillKey{made: true}}
	big := "SAY REF " + strings.Repeat("and say it well. ", 300*1024/17)
	if _, err := send.Run(context.Background(), application.GristSendRequest{
		App: "lampas", Kind: "tutor", RequestFile: request, Talk: "eval:greets:ab12cd34",
		GrindOverride: &application.GristGrindOverride{Instructions: big, Schema: `{"type": "object", "required": ["answer"]}`},
	}); err != nil {
		t.Fatalf("expected a text over the cap to be sent, got %v", err)
	}
	if len(outbox.Delivered()) != 1 || len(outbox.Uploaded()) != 1 {
		t.Fatalf("expected one message and one blob, got %d and %d", len(outbox.Delivered()), len(outbox.Uploaded()))
	}

	// What the backend holds, the mill reads.
	var payload application.PosternPayload
	if err := json.Unmarshal(outbox.Delivered()[0], &payload); err != nil {
		t.Fatal(err)
	}
	inbox := apptest.NewFakePostern()
	inbox.SetBlob(sha(outbox.Uploaded()[0]), outbox.Uploaded()[0])
	inbox.AddRecord(application.PosternRecord{Txid: "direct:g9", Class: application.GristClass, From: blobFactoryKey, To: millKey,
		Signer: blobFactoryKey, SignerApps: []string{"lampas"}, Ciphertext: payload.Ct})
	mill.Postern = inbox
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	answer := openFakeAnswer(t, inbox.Delivered()[0])
	calls := grinder.Calls()
	if answer.Status != application.GristAnswered || !answer.Grind.Override || len(calls) != 1 ||
		!strings.Contains(calls[0].Call.System, "SAY REF and say it well.") || strings.Contains(calls[0].Call.System, "SAY MAIN") ||
		strings.Contains(calls[0].Call.Schema, "items") {
		t.Fatalf("expected the grind run with the blob's text, got %+v and %d calls", answer, len(calls))
	}
	if inbox.HasBlob(sha(outbox.Uploaded()[0])) {
		t.Errorf("expected the answered grist's override blob deleted")
	}
}

// A small text still rides in the message.
func TestARefsTextUnderTheLimitStaysInTheMessage(t *testing.T) {
	send, backend, req := aGristSend(t)
	req.Photos = nil
	req.Talk = "eval:greets:ab12cd34"
	req.GrindOverride = &application.GristGrindOverride{Instructions: "SAY REF", Schema: "{}"}
	if _, err := send.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(backend.Uploaded()) != 0 {
		t.Errorf("expected nothing uploaded for a small text, got %d blobs", len(backend.Uploaded()))
	}
}

func mustBase64Decode(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// blobGrist puts on mill's backend one grist from `from` in `talk` naming an
// override blob sealed by sealedBy. The grist announces the hash of the body it
// means; with tampered, the backend holds another body under that hash.
func blobGrist(t *testing.T, mill *application.GristGrind, from, talk, sealedBy string, tampered bool) *apptest.FakePostern {
	t.Helper()
	millKey, _, _ := (&fakeMillKey{made: true}).PublicKey()
	override, _ := json.Marshal(application.GristGrindOverride{Instructions: "SAY REF", Schema: `{"type": "object", "required": ["answer"]}`})
	sealed, _ := (&apptest.FakeCipher{From: sealedBy}).EncryptBytes(millKey, override)
	raw := mustBase64Decode(t, sealed)
	announced := sha(raw)
	backend := apptest.NewFakePostern()
	if tampered {
		other, _ := (&apptest.FakeCipher{From: sealedBy}).EncryptBytes(millKey, []byte(`{"instructions":"SAY MAIN","schema":"{}"}`))
		backend.SetBlob(announced, mustBase64Decode(t, other))
	} else {
		backend.SetBlob(announced, raw)
	}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "lampas", Kind: "tutor", V: "1", Talk: talk}, Input: json.RawMessage(`{"question":"hi"}`),
		GrindOverrideBlob: &application.PosternAttachment{Hash: announced, Size: int64(len(raw)), Mime: application.GristOverrideMime},
	})
	ct, _ := (&apptest.FakeCipher{From: from}).Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:g1", Class: application.GristClass, From: from, To: millKey,
		Signer: from, SignerApps: []string{"lampas"}, Ciphertext: ct})
	mill.Postern = backend
	return backend
}

func TestTheMillGrindsWithAnOverrideBlobFromTheTestKey(t *testing.T) {
	mill, _, grinder := overrideMill(t, "factory", "eval:greets:ab12cd34")
	backend := blobGrist(t, &mill, blobFactoryKey, "eval:greets:ab12cd34", blobFactoryKey, false)
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	answer := openFakeAnswer(t, backend.Delivered()[0])
	calls := grinder.Calls()
	if answer.Status != application.GristAnswered || !answer.Grind.Override || len(calls) != 1 || !strings.HasSuffix(calls[0].Call.System, "SAY REF") {
		t.Fatalf("expected the blob's text ground, got %+v and %+v", answer, calls)
	}
}

// A blob that is not the one announced, or not sealed by the grist's sender,
// is refused, never ground with main's text.
func TestAnOverrideBlobThatIsNotTheOneAnnouncedIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		sealedBy string
		tampered bool
	}{
		"a body that does not hash to the announced hash": {blobFactoryKey, true},
		"a blob sealed by another key":                    {blobPhoneKey, false},
	} {
		t.Run(name, func(t *testing.T) {
			mill, _, grinder := overrideMill(t, "factory", "eval:greets:ab12cd34")
			backend := blobGrist(t, &mill, blobFactoryKey, "eval:greets:ab12cd34", tc.sealedBy, tc.tampered)
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != application.GristReasonOverrideForged || answer.Grind.Override {
				t.Errorf("expected the grist refused for a forged override blob, got %+v", answer)
			}
			if len(grinder.Calls()) != 0 {
				t.Errorf("expected nothing ground, got %d grinds", len(grinder.Calls()))
			}
		})
	}
}

// The rule for text a grist brings holds for a blob too.
func TestALiveAppsGristIsNeverGroundWithABlobOfTextItBrings(t *testing.T) {
	for name, tc := range map[string]struct{ from, key, talk string }{
		"an app's key, no talk":            {"phone", blobPhoneKey, ""},
		"an app's key, an ordinary talk":   {"phone", blobPhoneKey, "chat:1"},
		"an app's key naming an eval talk": {"phone", blobPhoneKey, "eval:greets:ab12cd34"},
		"the test key, no talk":            {"factory", blobFactoryKey, ""},
		"the test key, an ordinary talk":   {"factory", blobFactoryKey, "chat:1"},
	} {
		t.Run(name, func(t *testing.T) {
			mill, _, grinder := overrideMill(t, tc.from, tc.talk)
			backend := blobGrist(t, &mill, tc.key, tc.talk, tc.key, false)
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != application.GristReasonOverride || answer.Grind.Override {
				t.Errorf("expected the grist refused for bringing its own text, got %+v", answer)
			}
			if len(grinder.Calls()) != 0 {
				t.Errorf("expected nothing ground, got %d grinds", len(grinder.Calls()))
			}
		})
	}
}

package application_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// gristVectorFile is postern's docs/fixtures/grist-vectors.json, copied: the
// file the backend's tests read too.
const gristVectorFile = "testdata/grist-vectors.json"

// gristVectorShapes is the vectors' grist plaintext and answers, raw.
type gristVectorShapes struct {
	Keys map[string]struct {
		PublicKeyHex string `json:"publicKeyHex"`
	} `json:"keys"`
	Grist   json.RawMessage            `json:"grist"`
	Answers map[string]json.RawMessage `json:"answers"`
}

func readGristVectors(t *testing.T) gristVectorShapes {
	t.Helper()
	raw, err := os.ReadFile(gristVectorFile)
	if err != nil {
		t.Fatal(err)
	}
	var v gristVectorShapes
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("reading %s: %v", gristVectorFile, err)
	}
	return v
}

func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The vectors' grist and every answer read into the mill's own types and
// write back byte for byte: the same fields, in the protocol's order.
func TestGristVectorsRoundTripThroughTheMillsTypes(t *testing.T) {
	v := readGristVectors(t)

	var plain application.GristPlaintext
	if err := json.Unmarshal(v.Grist, &plain); err != nil {
		t.Fatalf("reading the vectors' grist: %v", err)
	}
	if plain.Grist != (application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}) || len(plain.Attachments) != 1 ||
		plain.Attachments[0].Mime != "image/jpeg" || plain.Attachments[0].Size != 412345 {
		t.Fatalf("the vectors' grist read as %+v", plain)
	}
	again, _ := application.GristJSON(plain)
	if got, want := string(again), compactJSON(t, v.Grist); got != want {
		t.Errorf("the grist wrote back as\n%s\nnot\n%s", got, want)
	}

	for _, status := range []string{application.GristAnswered, application.GristRefused, application.GristFailed} {
		raw, ok := v.Answers[status]
		if !ok {
			t.Fatalf("the vectors have no %s answer", status)
		}
		var answer application.GristAnswer
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatalf("reading the %s answer: %v", status, err)
		}
		if answer.Status != status || !strings.HasPrefix(answer.Re, "direct:") {
			t.Errorf("the %s answer read as %+v", status, answer)
		}
		again, _ := application.GristJSON(answer)
		if got, want := string(again), compactJSON(t, raw); got != want {
			t.Errorf("the %s answer wrote back as\n%s\nnot\n%s", status, got, want)
		}
	}
}

// A fingerprint is the installer's: sha256 of the key's hex text, the first
// 16 hex digits, in groups of four (sha256sum | cut -c1-16).
func TestKeyFingerprintIsTheInstallersOwn(t *testing.T) {
	v := readGristVectors(t)
	for name, want := range map[string]string{"mill": "1848 4c0e 494b bb6e", "cairnPhone": "9946 bb84 db1a 672b"} {
		if got := application.KeyFingerprint(v.Keys[name].PublicKeyHex); got != want {
			t.Errorf("the %s key's fingerprint is %q, want %q", name, got, want)
		}
	}
}

// A session run with a JSON schema reports its answer as structured_output,
// and why the model stopped.
func TestReadSessionResultReadsTheStructuredAnswer(t *testing.T) {
	result, err := application.ReadSessionResult(`{"type":"result","subtype":"success","is_error":false,"num_turns":4,
		"result":"","stop_reason":"end_turn","total_cost_usd":0.027,"permission_denials":[{"tool_name":"Read"}],
		"usage":{"input_tokens":3,"output_tokens":400},
		"structured_output":{"schemaVersion":"1.1","items":[{"name":"scissors"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Answer) != `{"schemaVersion":"1.1","items":[{"name":"scissors"}]}` || result.StopReason != "end_turn" ||
		result.Denials != 1 || !result.Finished() {
		t.Fatalf("read %+v", result)
	}
	none, _ := application.ReadSessionResult(`{"subtype":"success","structured_output":null}`)
	if none.Answer != nil {
		t.Fatalf("a null structured_output is no answer, got %s", none.Answer)
	}
}

// fakeMillKey is a PosternKeyFile that holds a key once Generate has run.
type fakeMillKey struct {
	made      bool
	generated int
}

func (k *fakeMillKey) Path() string          { return "/home/gov/.config/mw/mill.key" }
func (k *fakeMillKey) Exists() (bool, error) { return k.made, nil }
func (k *fakeMillKey) Generate() error       { k.made, k.generated = true, k.generated+1; return nil }
func (k *fakeMillKey) PublicKey() (string, string, error) {
	return "034f355bdcb7cc0af728ef3cceb9615d90684bb5b2ca5f859ab0f0b704075871aa", "mAddr", nil
}
func (k *fakeMillKey) PrivateKeyWIF() (string, error) { return "the-private-key", nil }
func (k *fakeMillKey) Sign([]application.PosternUtxo, []byte) (string, error) {
	return "", errors.New("the mill signs no transaction")
}
func (k *fakeMillKey) MarkSpent([]application.PosternUtxo) error { return nil }
func (k *fakeMillKey) MarkSent(string) error                     { return nil }

// mw grist key makes the key once, then only says its public half.
func TestGristKeyMakesTheKeyOnceAndPrintsItsPublicHalf(t *testing.T) {
	keys := &fakeMillKey{}
	var out bytes.Buffer
	first, err := application.GristKey{Keys: keys, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := application.GristKey{Keys: keys, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !first.Made || second.Made || keys.generated != 1 {
		t.Fatalf("expected the key made once, got %+v then %+v (%d generated)", first, second, keys.generated)
	}
	printed := out.String()
	for _, want := range []string{"public key: 034f355b", "fingerprint: 1848 4c0e 494b bb6e", "POSTERN_MILL_KEY=034f355b"} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected %q in:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "the-private-key") {
		t.Fatalf("the private key was printed:\n%s", printed)
	}
}

// A grind that runs out of time is answered failed, saying so; a model that
// declines is answered refused. Neither answer carries anything else.
func TestGristGrindSaysWhyAGrindDidNotAnswer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result application.SessionResult
		err    error
		status string
		reason string
	}{
		{"timed out", application.SessionResult{}, errors.Join(application.ErrGrindTimedOut), application.GristFailed, application.GristReasonTimedOut},
		{"declined", application.SessionResult{Subtype: "success", StopReason: "refusal"}, nil, application.GristRefused, application.GristReasonDeclined},
		{"answer missing a required field", application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[]}`)}, nil, application.GristFailed, application.GristReasonNoAnswer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mill, backend, grinder := aSmallMill(t)
			grinder.Result, grinder.Err = tc.result, tc.err
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			delivered := backend.Delivered()
			if len(delivered) != 1 {
				t.Fatalf("expected one answer, got %d", len(delivered))
			}
			answer := openFakeAnswer(t, delivered[0])
			if answer.Status != tc.status || answer.Reason != tc.reason || answer.Answer != nil {
				t.Fatalf("expected %s because %q, got %+v", tc.status, tc.reason, answer)
			}
		})
	}
}

// aSmallMill is a mill with one grist from a licensed phone waiting, one
// photo and no story running.
func aSmallMill(t *testing.T) (application.GristGrind, *apptest.FakePostern, *apptest.FakeGrinder) {
	t.Helper()
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	keys := &fakeMillKey{made: true}
	mill, _, _ := keys.PublicKey()
	backend := apptest.NewFakePostern()
	sealer := &apptest.FakeCipher{From: phone}
	photo, _ := sealer.EncryptBytes(mill, []byte("a photo"))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, photo))
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}, Input: json.RawMessage(`{"place":"Top drawer"}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "image/webp"}},
	})
	ct, _ := sealer.Encrypt(mill, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:g1", Class: application.GristClass, From: phone, To: mill,
		Signer: phone, SignerApps: []string{"cairn"}, Ciphertext: ct})

	grinds := apptest.NewFakeGrinds()
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.json", []byte(`{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],
		"model":"sonnet","effort":"low","instructions":"grinds/sweep.md","answerSchema":"schema.json",
		"attachments":{"min":1,"max":4,"mime":["image/webp"],"maxBytes":1000}}`))
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.md", []byte("List what you see."))
	grinds.SetFile("/rigs/cairn", "c0ffee", "schema.json", []byte(`{"type":"object","required":["items","placeName"]}`))
	grinder := &apptest.FakeGrinder{}
	return application.GristGrind{
		Postern: backend, Cipher: &apptest.FakeCipher{From: mill}, Keys: keys,
		State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: grinder, Tracker: apptest.NewFakeTracker(),
		Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop", Cap: 1,
		Apps: map[string]string{"cairn": "/rigs/cairn"}, TempDir: t.TempDir(),
		Now: func() time.Time { return time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC) },
	}, backend, grinder
}

func mustBase64(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// openFakeAnswer opens an answer the fake cipher sealed.
func openFakeAnswer(t *testing.T, payload []byte) application.GristAnswer {
	t.Helper()
	var p application.PosternPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	text, _, err := (&apptest.FakeCipher{}).Decrypt("any", p.Ct)
	if err != nil {
		t.Fatal(err)
	}
	var answer application.GristAnswer
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

// A photo sealed by some other key than the grist's own sender is refused,
// and never deleted: it is not the sender's to have deleted.
func TestGristGrindRefusesAPhotoSealedBySomebodyElse(t *testing.T) {
	mill, backend, grinder := aSmallMill(t)
	records, _ := backend.Messages(context.Background(), 0)
	var plain application.GristPlaintext
	text, _, _ := (&apptest.FakeCipher{}).Decrypt("any", records[0].Ciphertext)
	if err := json.Unmarshal([]byte(text), &plain); err != nil {
		t.Fatal(err)
	}
	stranger := &apptest.FakeCipher{From: "032c0b7cf95324a07d05398b240174dc0c2be444d96b159aa6c7f7b1e668680991"}
	sealed, _ := stranger.EncryptBytes(records[0].To, []byte("someone else's photo"))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
	plain.Attachments[0].Hash, plain.Attachments[0].Size = hash, size
	again, _ := json.Marshal(plain)
	phone := &apptest.FakeCipher{From: records[0].From}
	ct, _ := phone.Encrypt(records[0].To, string(again))
	backend.AddRecord(application.PosternRecord{Txid: "direct:g2", Class: application.GristClass, From: records[0].From,
		To: records[0].To, Signer: records[0].From, SignerApps: []string{"cairn"}, Ciphertext: ct})
	grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}

	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	delivered := backend.Delivered()
	if len(delivered) != 2 {
		t.Fatalf("expected two answers, got %d", len(delivered))
	}
	if answer := openFakeAnswer(t, delivered[1]); answer.Status != application.GristRefused || answer.Reason != application.GristReasonPhotoForged {
		t.Fatalf("expected the second grist refused for its photo, got %+v", answer)
	}
	if !backend.HasBlob(hash) {
		t.Fatalf("the photo sealed by somebody else was deleted")
	}
}

// A grind that names a file outside its rig, or an effort the harness does
// not have, cannot be read, and is refused without a session.
func TestGristGrindRefusesAGrindItCannotTrust(t *testing.T) {
	for name, grind := range map[string]string{
		"instructions outside the rig": `{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],"model":"sonnet","effort":"low",
			"instructions":"../../etc/passwd","answerSchema":"schema.json","attachments":{"min":0,"max":4}}`,
		"an unknown effort": `{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],"model":"sonnet","effort":"extreme",
			"instructions":"grinds/sweep.md","answerSchema":"schema.json","attachments":{"min":0,"max":4}}`,
		"another app's grind": `{"grind":1,"app":"spellforge","kind":"sweep","versions":["1.1"],"model":"sonnet","effort":"low",
			"instructions":"grinds/sweep.md","answerSchema":"schema.json","attachments":{"min":0,"max":4}}`,
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder := aSmallMill(t)
			mill.Grinds.(*apptest.FakeGrinds).SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.json", []byte(grind))
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != application.GristReasonBrokenGrind || len(grinder.Calls()) != 0 {
				t.Fatalf("expected a refusal as a broken grind and no session, got %+v", answer)
			}
		})
	}
}

const (
	gristPhoneKey    = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	gristStrangerKey = "032c0b7cf95324a07d05398b240174dc0c2be444d96b159aa6c7f7b1e668680991"
)

// A grist the mill cannot open, whose payload claims somebody else's key as
// its sender, is not counted against that somebody: the payload's own "from"
// is anyone's to write. It is counted against the key the backend proved
// signed it, or against nobody when the backend proved none.
func TestGristGrindDoesNotCountAnUnopenableGristAgainstItsClaimedSender(t *testing.T) {
	for name, signer := range map[string]string{"a proven signer": gristStrangerKey, "no proven signer": ""} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder := aSmallMill(t)
			mill.Ceilings = application.GristCeilings{DailyLimit: 2}
			grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
			records, _ := backend.Messages(context.Background(), 0)
			// After the phone's first grist, junk claiming its key, then a second grist of its own
			// (its photo is gone by then, which fails it: only a refusal is the limit).
			backend.AddRecord(application.PosternRecord{Txid: "direct:junk", Class: application.GristClass, From: gristPhoneKey,
				To: mustMillKey(t, mill), Signer: signer, Ciphertext: "not a ciphertext"})
			second := records[0]
			second.Txid, second.Seq = "direct:g3", 0
			backend.AddRecord(second)

			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			lines, _ := mill.State.Lines(context.Background())
			if len(lines) != 3 || lines[1].Txid != "direct:junk" || lines[1].Status != application.GristRefused {
				t.Fatalf("expected the junk refused second, got %+v", lines)
			}
			want := ""
			if signer != "" {
				want = application.KeyFingerprint(signer)
			}
			if lines[1].Sender != want {
				t.Errorf("the junk was counted against %q, want %q", lines[1].Sender, want)
			}
			if lines[2].Status == application.GristRefused {
				t.Fatalf("the phone's own second grist was refused after junk claiming its key: %+v", lines[2])
			}
		})
	}
}

func mustMillKey(t *testing.T, mill application.GristGrind) string {
	t.Helper()
	key, _, err := mill.Keys.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// aGristNaming is a grist from sender, sealed with cipher's identity, that
// names the blob hash as its one photo, added to backend as txid.
func aGristNaming(t *testing.T, mill application.GristGrind, backend *apptest.FakePostern, txid, claimedFrom string, sealer *apptest.FakeCipher, signerApps []string, hash string, size int64) {
	t.Helper()
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}, Input: json.RawMessage(`{}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "image/webp"}},
	})
	millKey := mustMillKey(t, mill)
	ct, _ := sealer.Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: txid, Class: application.GristClass, From: claimedFrom, To: millKey,
		Signer: claimedFrom, SignerApps: signerApps, Ciphertext: ct})
}

// A refused grist has the photos it names deleted only when the pass proved
// the grist's own sender sealed them: a grist that names somebody else's
// photo, refused before any photo is opened, leaves it where it is.
func TestGristGrindLeavesAPhotoItCouldNotProveTheSenderSealed(t *testing.T) {
	stranger := &apptest.FakeCipher{From: gristStrangerKey}
	phone := &apptest.FakeCipher{From: gristPhoneKey}
	for name, tc := range map[string]struct {
		claimedFrom string // the payload's "from"
		signerApps  []string
	}{
		"refused for its licence":      {gristPhoneKey, nil},
		"refused, its sender unproven": {gristStrangerKey, []string{"cairn"}},
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, _ := aSmallMill(t)
			records, _ := backend.Messages(context.Background(), 0)
			sealed, _ := stranger.EncryptBytes(records[0].To, []byte("the stranger's photo"))
			hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
			aGristNaming(t, mill, backend, "direct:g2", tc.claimedFrom, phone, tc.signerApps, hash, size)

			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if answer := openFakeAnswer(t, backend.Delivered()[1]); answer.Status != application.GristRefused {
				t.Fatalf("expected the grist refused, got %+v", answer)
			}
			if !backend.HasBlob(hash) {
				t.Fatalf("a photo the sender did not seal was deleted: %v", backend.Deleted())
			}
		})
	}
}

// A refused grist's own photos, sealed by its own sender, are still deleted
// once the pass has proved them.
func TestGristGrindDeletesTheSendersOwnPhotosOfARefusedGrist(t *testing.T) {
	mill, backend, _ := aSmallMill(t)
	records, _ := backend.Messages(context.Background(), 0)
	phone := &apptest.FakeCipher{From: gristPhoneKey}
	sealed, _ := phone.EncryptBytes(records[0].To, []byte("the phone's photo"))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
	aGristNaming(t, mill, backend, "direct:g2", gristPhoneKey, phone, nil, hash, size)

	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if backend.HasBlob(hash) {
		t.Fatalf("the sender's own photo was left on the backend")
	}
}

package application_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

// mw grist key makes nothing without Create; with it, the key is made once,
// then only its public half is said.
func TestGristKeyMakesTheKeyOnlyWhenAskedAndOnlyOnce(t *testing.T) {
	keys := &fakeMillKey{}
	var out bytes.Buffer
	home := &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop\n"}
	if _, err := (application.GristKey{Keys: keys, Out: &out}).Run(context.Background()); err == nil || keys.generated != 0 || out.Len() != 0 {
		t.Fatalf("expected a refusal that made and printed nothing, got %v (%d generated)\n%s", err, keys.generated, out.String())
	}
	create := application.GristKey{Keys: keys, Out: &out, Create: true, Home: home, Host: "laptop"}
	first, err := create.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := create.Run(context.Background())
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

// Create makes the key on the home host only: another host, a home that cannot
// be told or no home file at all is refused, until ForceHost is given.
func TestGristKeyCreateMakesTheKeyOnlyOnHomeUnlessForced(t *testing.T) {
	for name, home := range map[string]application.HomeFile{
		"another host is home": &apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop\n"},
		"the home is unknown":  &apptest.FakeHomeFile{Missing: true},
		"no home file given":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			keys := &fakeMillKey{}
			use := application.GristKey{Keys: keys, Create: true, Home: home, Host: "cloud1"}
			if _, err := use.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "--force-host") || keys.generated != 0 {
				t.Fatalf("expected a refusal naming --force-host, got %v (%d generated)", err, keys.generated)
			}
			use.ForceHost = true
			if _, err := use.Run(context.Background()); err != nil || keys.generated != 1 {
				t.Fatalf("expected --force-host to make the key, got %v (%d generated)", err, keys.generated)
			}
		})
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
		// A session the harness stopped at the account's usage limit is no fault
		// of the grist's: failed, so its sender may send it again, saying when
		// (mw-gq6.436).
		{"usage limit", application.SessionResult{Subtype: "success", IsError: true, Turns: 1, Said: "You've hit your weekly limit · resets Oct 16, 9am (America/New_York)"}, nil,
			application.GristFailed, "The factory cannot grind just now: it has reached its usage limit; send it again after 2026-10-16 13:00Z."},
		{"usage limit in the harness's last words", application.SessionResult{}, errors.New("claude: exit status 1: You've hit your session limit · resets 3pm (UTC)"),
			application.GristFailed, "The factory cannot grind just now: it has reached its usage limit; send it again after 2026-09-29 15:00Z."},
		{"overloaded", application.SessionResult{IsError: true, Said: `API Error: 529 {"type":"error","error":{"type":"overloaded_error"}}`}, nil,
			application.GristFailed, "The factory cannot grind just now: its model is overloaded; send it again after 2026-09-29 10:15Z."},
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
		State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: grinder,
		Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop",
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

// A grind whose instructions name a directory is read as that directory's
// *.md parts in name order, one blank line between them; one naming a file
// is read as it always was; a directory with no parts is refused as empty
// instructions are.
func TestGristGrindReadsInstructionsFromADirectoryOfParts(t *testing.T) {
	const dir = "grinds/sweep.instructions"
	withInstructions := func(mill application.GristGrind, parts map[string]string) {
		grinds := mill.Grinds.(*apptest.FakeGrinds)
		grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.json", []byte(`{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],
			"model":"sonnet","effort":"low","instructions":"`+dir+`","answerSchema":"schema.json",
			"attachments":{"min":1,"max":4,"mime":["image/webp"],"maxBytes":1000}}`))
		for name, text := range parts {
			grinds.SetFile("/rigs/cairn", "c0ffee", dir+"/"+name, []byte(text))
		}
	}
	t.Run("three parts", func(t *testing.T) {
		mill, _, grinder := aSmallMill(t)
		withInstructions(mill, map[string]string{"20-second.md": "Second.\n", "10-first.md": "First.\n", "30-third.md": "Third.", "README.txt": "no"})
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		calls := grinder.Calls()
		if len(calls) != 1 || !strings.HasSuffix(calls[0].Call.System, "First.\n\nSecond.\n\nThird.") {
			t.Fatalf("expected one session with the three parts joined in name order, got %+v", calls)
		}
	})
	t.Run("one file", func(t *testing.T) {
		mill, _, grinder := aSmallMill(t)
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		calls := grinder.Calls()
		if len(calls) != 1 || !strings.HasSuffix(calls[0].Call.System, "\n\nList what you see.") {
			t.Fatalf("expected one session with the file's text, got %+v", calls)
		}
	})
	t.Run("no parts", func(t *testing.T) {
		mill, backend, grinder := aSmallMill(t)
		withInstructions(mill, map[string]string{"notes.txt": "no .md here"})
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		answer := openFakeAnswer(t, backend.Delivered()[0])
		if answer.Status != application.GristRefused || answer.Reason != application.GristReasonBrokenGrind || len(grinder.Calls()) != 0 {
			t.Fatalf("expected a refusal as a broken grind and no session, got %+v", answer)
		}
	})
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

// aGristWithPhotos adds a grist from the phone carrying n photos, sealed by the
// phone, to backend as txid.
func aGristWithPhotos(t *testing.T, mill application.GristGrind, backend *apptest.FakePostern, txid string, n int) {
	t.Helper()
	phone := &apptest.FakeCipher{From: gristPhoneKey}
	millKey := mustMillKey(t, mill)
	var photos []application.PosternAttachment
	for i := 0; i < n; i++ {
		sealed, _ := phone.EncryptBytes(millKey, []byte(fmt.Sprintf("photo %s %d", txid, i)))
		hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
		photos = append(photos, application.PosternAttachment{Hash: hash, Size: size, Mime: "image/webp"})
	}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}, Input: json.RawMessage(`{}`), Attachments: photos,
	})
	ct, _ := phone.Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: txid, Class: application.GristClass, From: gristPhoneKey, To: millKey,
		Signer: gristPhoneKey, SignerApps: []string{"cairn"}, Ciphertext: ct})
}

// The mill's record of a grist says how many photos it carried, since the
// photos are deleted once the answer is delivered: 0 is an answer, so the
// field is written for a grist with none too.
func TestGristGrindRecordsHowManyPhotosEachGristCarried(t *testing.T) {
	mill, backend, grinder := aSmallMill(t)
	mill.Ceilings = application.GristCeilings{DailyLimit: 10}
	grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
	mill.Grinds.(*apptest.FakeGrinds).SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.json", []byte(`{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],
		"model":"sonnet","effort":"low","instructions":"grinds/sweep.md","answerSchema":"schema.json",
		"attachments":{"min":0,"max":4,"mime":["image/webp"],"maxBytes":1000}}`))
	aGristWithPhotos(t, mill, backend, "direct:g2", 2)
	aGristWithPhotos(t, mill, backend, "direct:g3", 0)

	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, _ := mill.State.Lines(context.Background())
	if len(lines) != 3 {
		t.Fatalf("expected three lines, got %+v", lines)
	}
	for i, want := range []string{`"photos":1`, `"photos":2`, `"photos":0`} {
		line, _ := json.Marshal(lines[i])
		if !strings.Contains(string(line), want) || lines[i].Status != application.GristAnswered {
			t.Errorf("line %d (%s) want %s and answered, got %s", i, lines[i].Txid, want, line)
		}
	}
}

// A grind's line names the model id the harness's result names beside the
// alias it was run with, and so does the timing of the run kept: after the
// alias moves, the record still says which model answered (mw-2zh33g.11). A
// result that names none writes no model_id at all.
func TestGristGrindRecordsTheModelIDTheResultNames(t *testing.T) {
	mill, _, grinder := aSmallMill(t)
	runs := &apptest.FakeGristRuns{}
	mill.Runs = runs
	grinder.Result = realResult(t, "result-haiku-grind.json")
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, _ := mill.State.Lines(context.Background())
	if len(lines) != 1 || lines[0].Status != application.GristAnswered {
		t.Fatalf("expected one answered line, got %+v", lines)
	}
	line, _ := json.Marshal(lines[0])
	for _, want := range []string{`"model":"sonnet"`, `"model_id":"claude-haiku-5-5"`} {
		if !strings.Contains(string(line), want) {
			t.Errorf("expected %s in the line, got %s", want, line)
		}
	}
	if len(runs.Kept) != 1 || runs.Kept[0].ModelID != "claude-haiku-5-5" || runs.Kept[0].Model != "sonnet" {
		t.Errorf("expected the kept run's timing to name sonnet and claude-haiku-5-5, got %+v", runs.Kept)
	}
	timing, _ := json.Marshal(runs.Kept[0])
	if !strings.Contains(string(timing), `"model_id":"claude-haiku-5-5"`) {
		t.Errorf("expected model_id in the kept timing, got %s", timing)
	}

	bare, _, bareGrinder := aSmallMill(t)
	bareGrinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
	if _, err := bare.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	bareLines, _ := bare.State.Lines(context.Background())
	if raw, _ := json.Marshal(bareLines); len(bareLines) != 1 || strings.Contains(string(raw), "model_id") {
		t.Errorf("expected one line with no model_id, got %s", raw)
	}
}

// A grind whose harness could not run (claude never started, say) is answered
// failed as before, but the record of the run keeps the error's words in its
// notes: nothing said why in the Lampas outage of mw-gq6.690, where claude's
// exec failed with "argument list too long" and the line held only a reason.
func TestGristGrindRecordsTheHarnessErrorInTheRunsNotes(t *testing.T) {
	mill, backend, grinder := aSmallMill(t)
	grinder.Err = errors.New("claude: fork/exec /usr/bin/claude: argument list too long")
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, err := mill.State.Lines(context.Background())
	if err != nil || len(lines) != 1 {
		t.Fatalf("expected one line, got %v, %v", lines, err)
	}
	if lines[0].Status != application.GristFailed || lines[0].Reason != application.GristReasonSessionEnd {
		t.Fatalf("expected the session ended, got %+v", lines[0])
	}
	if got := strings.Join(lines[0].Notes, "\n"); !strings.Contains(got, "argument list too long") {
		t.Fatalf("expected the error's text in the notes, got %q", got)
	}
	if answer := openFakeAnswer(t, backend.Delivered()[0]); strings.Contains(answer.Reason, "argument list") {
		t.Fatalf("expected the sender told nothing of the mill's inside, got %q", answer.Reason)
	}
}

// aReadingMill is a mill with one grist waiting: a recording read aloud, asking
// for lang ("" asks for none), against a grind whose scoring.langs is langs.
// The engine "local" is a fake that scores any reading.
func aReadingMill(t *testing.T, langs, lang string) (application.GristGrind, *apptest.FakePostern, *apptest.FakeGrinder, *apptest.FakeScorer) {
	t.Helper()
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	keys := &fakeMillKey{made: true}
	mill, _, _ := keys.PublicKey()
	backend := apptest.NewFakePostern()
	sealer := &apptest.FakeCipher{From: phone}
	clip, _ := sealer.EncryptBytes(mill, []byte("webm bytes"))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, clip))
	input := `{"schemaVersion":"1.1","mode":"reading","target_text":"בְּרֵאשִׁית"`
	if lang != "" {
		input += `,"lang":"` + lang + `"`
	}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "reading", V: "1.1"}, Input: json.RawMessage(input + `}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "audio/webm"}},
	})
	ct, _ := sealer.Encrypt(mill, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:r1", Class: application.GristClass, From: phone, To: mill,
		Signer: phone, SignerApps: []string{"cairn"}, Ciphertext: ct})

	grinds := apptest.NewFakeGrinds()
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/reading.json", []byte(`{"grind":1,"app":"cairn","kind":"reading","versions":["1.1"],
		"model":"sonnet","effort":"low","instructions":"grinds/reading.md","answerSchema":"schema.json",
		"attachments":{"min":1,"max":2,"mime":["audio/webm"],"maxBytes":1000},
		"scoring":{"audio":true,"target_field":"target_text","langs":`+langs+`}}`))
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/reading.md", []byte("Say how it went."))
	grinds.SetFile("/rigs/cairn", "c0ffee", "schema.json", []byte(`{"type":"object"}`))
	grinder := &apptest.FakeGrinder{Result: application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{}`)}}
	local := &apptest.FakeScorer{Result: application.ReadingResult{Engine: "local", Accuracy: 90, Words: []application.ReadingWord{{Text: "בְּרֵאשִׁית", Accuracy: 90}}}}
	return application.GristGrind{
		Postern: backend, Cipher: &apptest.FakeCipher{From: mill}, Keys: keys,
		State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: grinder,
		Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop",
		Scorers: application.NewScorerRegistry(map[string]application.Scorer{"local": local}),
		Apps:    map[string]string{"cairn": "/rigs/cairn"}, TempDir: t.TempDir(),
		Now: func() time.Time { return time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC) },
	}, backend, grinder, local
}

// Hebrew is a language a grind may score in and a grist may ask for; French
// still is not, in the words it always was refused with (mw-gdcyf98.8).
func TestGristGrindScoresHebrewWhenTheGrindNamesIt(t *testing.T) {
	t.Run("a grind naming he, a grist asking for he", func(t *testing.T) {
		mill, backend, _, local := aReadingMill(t, `["en","el","he"]`, "he")
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		delivered := backend.Delivered()
		if len(delivered) != 1 {
			t.Fatalf("expected one answer, got %d", len(delivered))
		}
		if answer := openFakeAnswer(t, delivered[0]); answer.Status != application.GristAnswered {
			t.Fatalf("expected the grist answered, got %+v", answer)
		}
		calls := local.Calls()
		if len(calls) != 1 || calls[0].Lang != "he" {
			t.Fatalf("expected the engine given the language he, got %+v", calls)
		}
	})
	t.Run("a grist asking for fr is refused as before", func(t *testing.T) {
		mill, backend, _, local := aReadingMill(t, `["en","el","he"]`, "fr")
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		answer := openFakeAnswer(t, backend.Delivered()[0])
		want := "This grist asks for the language fr; its grind scores in en, el and he."
		if answer.Status != application.GristRefused || answer.Reason != want {
			t.Fatalf("expected refused because %q, got %+v", want, answer)
		}
		if len(local.Calls()) != 0 {
			t.Errorf("no engine should be given a recording, got %+v", local.Calls())
		}
	})
	t.Run("a grind naming fr is broken", func(t *testing.T) {
		mill, backend, _, _ := aReadingMill(t, `["en","fr"]`, "")
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		answer := openFakeAnswer(t, backend.Delivered()[0])
		if answer.Status != application.GristRefused || answer.Reason != application.GristReasonBrokenGrind {
			t.Fatalf("expected refused as a broken grind, got %+v", answer)
		}
	})
}

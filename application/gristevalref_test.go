package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// textSender is a mill that follows the instructions it is brought: it answers
// "ref" when the send carries instructions saying SAY REF, and "main" otherwise,
// and says it ground with the override only when it was sent one.
type textSender struct {
	sent []application.GristSendRequest
}

func (s *textSender) Run(_ context.Context, req application.GristSendRequest) (application.GristSendReport, error) {
	s.sent = append(s.sent, req)
	answer, override := "main", req.GrindOverride != nil
	if override && strings.Contains(req.GrindOverride.Instructions, "SAY REF") {
		answer = "ref"
	}
	return application.GristSendReport{Txid: "direct:t", Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(`{"answer": "` + answer + `"}`),
		Grind: application.GristAnswerGrind{Override: override},
	}}, nil
}

const refExample = `{"schemaVersion": "1", "request": {"question": "hi"}, "expect": {"answer": {"matches": "ref"}}}`

// evalRig is a lampas rig whose main says SAY MAIN and whose branch says SAY REF.
func evalRig(t *testing.T, sender application.GristSmokeSender) (application.GristSmoke, *apptest.FakeGrinds) {
	t.Helper()
	const checkout, commit, refCommit = "/rigs/lampas", "c1", "c2"
	grinds := apptest.NewFakeGrinds()
	grind := `{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "schemas/tutor.json"}`
	schema := `{"type": "object", "required": ["answer"], "properties": {"answer": {"type": "string"}}}`
	for _, f := range []struct{ path, main, ref string }{
		{"grinds/tutor.json", grind, grind},
		{"grinds/tutor.md", "SAY MAIN", "SAY REF"},
		{"schemas/tutor.json", schema, schema},
		{"grinds/examples/tutor/greets.json", refExample, refExample},
	} {
		grinds.SetFile(checkout, commit, f.path, []byte(f.main))
		grinds.SetRefFile(checkout, "mw/story", refCommit, f.path, []byte(f.ref))
	}
	return application.GristSmoke{
		Grinds: grinds, Send: sender, Apps: map[string]string{"lampas": checkout}, TempDir: t.TempDir(),
		Sleep: func(context.Context, time.Duration) error { return nil },
	}, grinds
}

func TestEvalWithARefGrindsWithTheRefsInstructionsAndWithoutItWithMains(t *testing.T) {
	sender := &textSender{}
	smoke, _ := evalRig(t, sender)
	ender := &talkEnder{}
	smoke.Talks = ender

	var out bytes.Buffer
	if err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story", Out: &out}).Run(context.Background(), "lampas", "tutor", []string{"greets"}); err != nil {
		t.Fatalf("expected the example to pass with --ref: %v\n%s", err, out.String())
	}
	if len(sender.sent) != 1 || sender.sent[0].GrindOverride == nil || sender.sent[0].GrindOverride.Instructions != "SAY REF" ||
		!strings.Contains(sender.sent[0].GrindOverride.Schema, `"required"`) {
		t.Fatalf("expected the ref's instructions and schema on the send, got %+v", sender.sent)
	}
	if !strings.HasPrefix(sender.sent[0].Talk, "eval:") || len(ender.ended) != 1 {
		t.Errorf("expected a one-turn example under an eval: talk that is ended, got %q and %v", sender.sent[0].Talk, ender.ended)
	}

	sender.sent = nil
	out.Reset()
	err := (application.GristEvalExamples{Smoke: smoke, Out: &out}).Run(context.Background(), "lampas", "tutor", []string{"greets"})
	if !errors.Is(err, application.ErrGristEvalFailed) {
		t.Fatalf("expected the example to fail without --ref, got %v\n%s", err, out.String())
	}
	if len(sender.sent) != 1 || sender.sent[0].GrindOverride != nil || sender.sent[0].Talk != "" {
		t.Errorf("expected nothing but the example sent without --ref, got %+v", sender.sent)
	}
}

func TestEvalReadsTheExampleAtTheRefToo(t *testing.T) {
	sender := &textSender{}
	smoke, grinds := evalRig(t, sender)
	grinds.SetRefFile("/rigs/lampas", "mw/story", "c2", "grinds/examples/tutor/new.json", []byte(refExample))
	if err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story"}).Run(context.Background(), "lampas", "tutor", []string{"new"}); err != nil {
		t.Fatalf("expected an example only the ref has to be sent: %v", err)
	}
	if err := (application.GristEvalExamples{Smoke: smoke}).Run(context.Background(), "lampas", "tutor", []string{"new"}); err == nil {
		t.Error("expected main, which lacks the example, to refuse it")
	}
}

func TestEvalWithARefNoRigHoldsIsRefusedBeforeAnythingIsSent(t *testing.T) {
	sender := &textSender{}
	smoke, _ := evalRig(t, sender)
	err := (application.GristEvalExamples{Smoke: smoke, Ref: "no/such"}).Run(context.Background(), "lampas", "tutor", []string{"greets"})
	if err == nil || !strings.Contains(err.Error(), "no/such") || len(sender.sent) != 0 {
		t.Errorf("expected a refusal naming the ref and nothing sent, got %v and %d sends", err, len(sender.sent))
	}
}

// A mill that does not know the override answers with main's grind; the eval
// says so, rather than pass or fail the example for the wrong text.
type oldMill struct{}

func (oldMill) Run(context.Context, application.GristSendRequest) (application.GristSendReport, error) {
	return application.GristSendReport{Txid: "direct:t", Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(`{"answer": "ref"}`),
	}}, nil
}

func TestEvalFailsWhenTheMillDidNotGrindWithTheRefsText(t *testing.T) {
	smoke, _ := evalRig(t, oldMill{})
	var out bytes.Buffer
	err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story", Out: &out}).Run(context.Background(), "lampas", "tutor", []string{"greets"})
	if !errors.Is(err, application.ErrGristEvalFailed) || !strings.Contains(out.String(), "not the ref's text") {
		t.Errorf("expected a failure saying the mill used main's grind, got %v\n%s", err, out.String())
	}
}

// overrideMill is a mill with one grist waiting, from the key `from` in the talk
// `talk`, carrying a grind override; testKey is the mill's test key.
func overrideMill(t *testing.T, from, talk string) (application.GristGrind, *apptest.FakePostern, *apptest.FakeGrinder) {
	t.Helper()
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	const factory = "03aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if from == "factory" {
		from = factory
	} else {
		from = phone
	}
	keys := &fakeMillKey{made: true}
	mill, _, _ := keys.PublicKey()
	backend := apptest.NewFakePostern()
	sealer := &apptest.FakeCipher{From: from}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "lampas", Kind: "tutor", V: "1", Talk: talk}, Input: json.RawMessage(`{"question":"hi"}`),
		GrindOverride: &application.GristGrindOverride{Instructions: "SAY REF", Schema: `{"type": "object", "required": ["answer"]}`},
	})
	ct, _ := sealer.Encrypt(mill, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: "direct:g1", Class: application.GristClass, From: from, To: mill,
		Signer: from, SignerApps: []string{"lampas"}, Ciphertext: ct})
	grinds := apptest.NewFakeGrinds()
	grinds.SetFile("/rigs/lampas", "c0ffee", "grinds/tutor.json", []byte(`{"grind":1,"app":"lampas","kind":"tutor","versions":["1"],
		"model":"sonnet","effort":"low","instructions":"grinds/tutor.md","answerSchema":"schema.json","attachments":{"min":0,"max":0}}`))
	grinds.SetFile("/rigs/lampas", "c0ffee", "grinds/tutor.md", []byte("SAY MAIN"))
	grinds.SetFile("/rigs/lampas", "c0ffee", "schema.json", []byte(`{"type":"object","required":["items"]}`))
	grinder := &apptest.FakeGrinder{Result: application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"answer":"ref"}`)}}
	return application.GristGrind{
		Postern: backend, Cipher: &apptest.FakeCipher{From: mill}, Keys: keys, TestKey: factory,
		State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: grinder,
		Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop",
		Apps: map[string]string{"lampas": "/rigs/lampas"}, TempDir: t.TempDir(),
		Credits: apptest.UsableCredits(),
		Now:     func() time.Time { return time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC) },
	}, backend, grinder
}

func TestTheMillGrindsAnEvalTalkFromTheTestKeyWithTheTextItBrings(t *testing.T) {
	for _, talk := range []string{"eval:greets:ab12cd34", "smoke:tutor/greets:ab12cd34"} {
		mill, backend, grinder := overrideMill(t, "factory", talk)
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		answer := openFakeAnswer(t, backend.Delivered()[0])
		if answer.Status != application.GristAnswered || !answer.Grind.Override {
			t.Fatalf("%s: expected the grist answered and said to be ground with the text it brought, got %+v", talk, answer)
		}
		calls := grinder.Calls()
		if len(calls) != 1 || !strings.HasSuffix(calls[0].Call.System, "SAY REF") || strings.Contains(calls[0].Call.System, "SAY MAIN") ||
			strings.Contains(calls[0].Call.Schema, "items") {
			t.Fatalf("%s: expected the grinder given the brought instructions and schema, got %+v", talk, calls)
		}
	}
}

// A live app's grist can never be given other instructions: not from an app's
// key in any talk, not from the test key outside an eval or smoke talk, and not
// by naming a talk eval: from a key that is not the factory's.
func TestALiveAppsGristIsNeverGroundWithTextItBrings(t *testing.T) {
	for name, tc := range map[string]struct{ from, talk string }{
		"an app's key, no talk":            {"phone", ""},
		"an app's key, an ordinary talk":   {"phone", "chat:1"},
		"an app's key naming an eval talk": {"phone", "eval:greets:ab12cd34"},
		"an app's key naming a smoke talk": {"phone", "smoke:greets:ab12cd34"},
		"the test key, no talk":            {"factory", ""},
		"the test key, an ordinary talk":   {"factory", "chat:1"},
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder := overrideMill(t, tc.from, tc.talk)
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

func TestAGristWithoutTextIsGroundWithMainsAsBefore(t *testing.T) {
	mill, _, grinder := overrideMill(t, "phone", "")
	// Re-seal the same grist without the override.
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	plain, _ := json.Marshal(application.GristPlaintext{Grist: application.GristName{App: "lampas", Kind: "tutor", V: "1"}, Input: json.RawMessage(`{"question":"hi"}`)})
	ct, _ := (&apptest.FakeCipher{From: phone}).Encrypt(millKey, string(plain))
	fresh := apptest.NewFakePostern()
	fresh.AddRecord(application.PosternRecord{Txid: "direct:g2", Class: application.GristClass, From: phone, To: millKey, Signer: phone, SignerApps: []string{"lampas"}, Ciphertext: ct})
	mill.Postern = fresh
	runs := &apptest.FakeGristRuns{}
	mill.Runs = runs
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	answer := openFakeAnswer(t, fresh.Delivered()[0])
	calls := grinder.Calls()
	if answer.Grind.Override || len(calls) != 1 || !strings.HasSuffix(calls[0].Call.System, "SAY MAIN") {
		t.Errorf("expected main's instructions, got %+v and %+v", answer, calls)
	}
	if len(runs.Kept) != 1 || runs.Kept[0].Factory {
		t.Errorf("an app key's run is not the factory's: %+v", runs.Kept)
	}
}

func TestASendOfARefsTextOutsideAnEvalOrSmokeTalkIsNotSent(t *testing.T) {
	dir := t.TempDir()
	request := dir + "/request.json"
	if err := os.WriteFile(request, []byte(`{"schemaVersion":"1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	send := application.GristSend{Postern: apptest.NewFakePostern(), Cipher: &apptest.FakeCipher{}, Keys: &fakeMillKey{made: true}}
	for _, talk := range []string{"", "chat:1"} {
		_, err := send.Run(context.Background(), application.GristSendRequest{
			App: "lampas", Kind: "tutor", RequestFile: request, Talk: talk,
			GrindOverride: &application.GristGrindOverride{Instructions: "x", Schema: "{}"},
		})
		if err == nil || !strings.Contains(err.Error(), "eval") {
			t.Errorf("talk %q: expected the send refused, saying eval or smoke talks only, got %v", talk, err)
		}
	}
}

// A run is kept as the factory's when its key is the mill's test key, so the
// room check does not take a smoke for a tutor in use.
func TestAGrindFromTheTestKeyIsKeptAsTheFactorys(t *testing.T) {
	for name, tc := range map[string]struct {
		from, talk string
		want       bool
	}{
		"the test key's eval":  {"factory", "eval:greets:ab12cd34", true},
		"the test key's smoke": {"factory", "smoke:tutor/greets:ab12cd34", true},
	} {
		t.Run(name, func(t *testing.T) {
			mill, _, _ := overrideMill(t, tc.from, tc.talk)
			runs := &apptest.FakeGristRuns{}
			mill.Runs = runs
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(runs.Kept) != 1 || runs.Kept[0].Factory != tc.want {
				t.Errorf("expected one run kept with Factory = %v, got %+v", tc.want, runs.Kept)
			}
		})
	}
}

// newKindMill is overrideMill for a kind main does not have: the grist names
// the kind "sorter", main holds only the tutor grind, and the grist brings the
// grind text of a ref: none, the instructions and schema alone, or those with
// the grind file too.
type bring int

const (
	bringNothing bring = iota
	bringText
	bringGrind
)

func newKindMill(t *testing.T, from, talk string, brings bring) (application.GristGrind, *apptest.FakePostern, *apptest.FakeGrinder) {
	t.Helper()
	mill, _, grinder := overrideMill(t, from, talk)
	const factory = "03aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	sender := phone
	if from == "factory" {
		sender = factory
	}
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	var override *application.GristGrindOverride
	if brings != bringNothing {
		override = &application.GristGrindOverride{Instructions: "SAY REF", Schema: `{"type": "object", "required": ["answer"]}`}
	}
	if brings == bringGrind {
		override.Grind = json.RawMessage(`{"grind":1,"app":"lampas","kind":"sorter","versions":["1"],
			"model":"sonnet","effort":"low","instructions":"grinds/sorter.md","answerSchema":"schemas/sorter.json","attachments":{"min":0,"max":0}}`)
	}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "lampas", Kind: "sorter", V: "1", Talk: talk}, Input: json.RawMessage(`{"question":"hi"}`),
		GrindOverride: override,
	})
	ct, _ := (&apptest.FakeCipher{From: sender}).Encrypt(millKey, string(plain))
	fresh := apptest.NewFakePostern()
	fresh.AddRecord(application.PosternRecord{Txid: "direct:g3", Class: application.GristClass, From: sender, To: millKey,
		Signer: sender, SignerApps: []string{"lampas"}, Ciphertext: ct})
	mill.Postern = fresh
	return mill, fresh, grinder
}

// A kind new to main is proved on its branch: an eval send brings the grind
// file with its text, and the mill grinds with it instead of refusing the kind
// (mw-gq6.860).
func TestTheMillGrindsAKindNewToMainWithTheGrindAnEvalBrings(t *testing.T) {
	for _, talk := range []string{"eval:greets:ab12cd34", "smoke:sorter/greets:ab12cd34"} {
		mill, backend, grinder := newKindMill(t, "factory", talk, bringGrind)
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		answer := openFakeAnswer(t, backend.Delivered()[0])
		if answer.Status != application.GristAnswered || !answer.Grind.Override {
			t.Fatalf("%s: expected the new kind answered with the text it brought, got %+v", talk, answer)
		}
		calls := grinder.Calls()
		if len(calls) != 1 || !strings.HasSuffix(calls[0].Call.System, "SAY REF") {
			t.Fatalf("%s: expected one grind with the brought instructions, got %+v", talk, calls)
		}
	}
}

// An ordinary send of a kind main lacks is refused as before, whoever sends it
// and whatever it brings: only the factory's test key in an eval or smoke talk
// is ground with the grind it brings.
func TestAKindMainLacksIsStillRefusedForAnOrdinarySend(t *testing.T) {
	for name, tc := range map[string]struct {
		from, talk, reason string
		brings             bring
	}{
		"an app's key, nothing brought":       {"phone", "", application.GristReasonNoGrind, bringNothing},
		"the test key, nothing brought":       {"factory", "chat:1", application.GristReasonNoGrind, bringNothing},
		"the test key, an eval talk, nothing": {"factory", "eval:greets:ab12cd34", application.GristReasonNoGrind, bringNothing},
		"an eval talk, text but no grind":     {"factory", "eval:greets:ab12cd34", application.GristReasonNoGrind, bringText},
		"an app's key bringing a grind":       {"phone", "", application.GristReasonOverride, bringGrind},
		"the test key, ordinary talk":         {"factory", "chat:1", application.GristReasonOverride, bringGrind},
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder := newKindMill(t, tc.from, tc.talk, tc.brings)
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != tc.reason {
				t.Errorf("expected the grist refused with %q, got %+v", tc.reason, answer)
			}
			if len(grinder.Calls()) != 0 {
				t.Errorf("expected nothing ground, got %d grinds", len(grinder.Calls()))
			}
		})
	}
}

// With --ref the eval sends the grind file at the ref along with its text, so a
// kind main lacks can be ground.
func TestEvalWithARefSendsTheGrindFileAtTheRef(t *testing.T) {
	sender := &textSender{}
	smoke, _ := evalRig(t, sender)
	if err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story"}).Run(context.Background(), "lampas", "tutor", []string{"greets"}); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || sender.sent[0].GrindOverride == nil || !strings.Contains(string(sender.sent[0].GrindOverride.Grind), `"kind": "tutor"`) {
		t.Fatalf("expected the grind file of the ref on the send, got %+v", sender.sent)
	}
}

// A smoke or eval talk marks its grist as the factory's even when the mill does
// not hold the test key that sealed it (a host whose grist_test_key_file is
// unset), so a machine burst never counts toward the slow-grist hold
// (mw-gq6.752). The run keeps which kind of talk it was.
func TestAGristInASmokeOrEvalTalkIsKeptAsTheFactorysWhoeverSealedIt(t *testing.T) {
	for name, tc := range map[string]struct {
		from, talk string
		factory    bool
		class      string
	}{
		"an app key in a smoke talk":    {"phone", "smoke:tutor/greets:ab12cd34", true, "smoke"},
		"an app key in an eval talk":    {"phone", "eval:greets:ab12cd34", true, "eval"},
		"the test key in a smoke talk":  {"factory", "smoke:tutor/greets:ab12cd34", true, "smoke"},
		"the test key in an eval talk":  {"factory", "eval:greets:ab12cd34", true, "eval"},
		"an app key in a reader's talk": {"phone", "chat:1", false, "reader"},
	} {
		t.Run(name, func(t *testing.T) {
			mill, _, _ := overrideMill(t, tc.from, tc.talk)
			if tc.from != "factory" {
				// An app key may not bring grind text: seal the grist again without it,
				// and let the mill not know the test key.
				const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
				keys := &fakeMillKey{made: true}
				millKey, _, _ := keys.PublicKey()
				plain, _ := json.Marshal(application.GristPlaintext{Grist: application.GristName{App: "lampas", Kind: "tutor", V: "1", Talk: tc.talk}, Input: json.RawMessage(`{"question":"hi"}`)})
				ct, _ := (&apptest.FakeCipher{From: phone}).Encrypt(millKey, string(plain))
				fresh := apptest.NewFakePostern()
				fresh.AddRecord(application.PosternRecord{Txid: "direct:g2", Class: application.GristClass, From: phone, To: millKey, Signer: phone, SignerApps: []string{"lampas"}, Ciphertext: ct})
				mill.Postern = fresh
				mill.TestKey = ""
			}
			runs := &apptest.FakeGristRuns{}
			mill.Runs = runs
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(runs.Kept) != 1 {
				t.Fatalf("expected one run kept, got %+v", runs.Kept)
			}
			got := runs.Kept[0]
			if got.Factory != tc.factory || got.SenderClass() != tc.class {
				t.Errorf("expected Factory = %v and class %q, got Factory = %v and class %q", tc.factory, tc.class, got.Factory, got.SenderClass())
			}
		})
	}
}

// newVersionMill is overrideMill for a version main does not list: main's grind
// for the tutor kind lists only 1.1, and the grist names the version v and
// brings, when refVersions is not empty, a grind file at the ref listing those.
// It brings the ref's model and effort in the grind file too, to see that main's
// are kept.
func newVersionMill(t *testing.T, from, talk, v string, refVersions []string) (application.GristGrind, *apptest.FakePostern, *apptest.FakeGrinder) {
	t.Helper()
	mill, _, grinder := overrideMill(t, from, talk)
	const factory = "03aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	sender := phone
	if from == "factory" {
		sender = factory
	}
	mill.Grinds.(*apptest.FakeGrinds).SetFile("/rigs/lampas", "c0ffee", "grinds/tutor.json", []byte(`{"grind":1,"app":"lampas","kind":"tutor","versions":["1.1"],
		"model":"sonnet","effort":"low","instructions":"grinds/tutor.md","answerSchema":"schema.json","attachments":{"min":0,"max":0}}`))
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	override := &application.GristGrindOverride{Instructions: "SAY REF", Schema: `{"type": "object", "required": ["answer"]}`}
	if len(refVersions) > 0 {
		versions, _ := json.Marshal(refVersions)
		override.Grind = json.RawMessage(`{"grind":1,"app":"lampas","kind":"tutor","versions":` + string(versions) + `,
			"model":"opus","effort":"high","instructions":"grinds/tutor.md","answerSchema":"schema.json","attachments":{"min":0,"max":0}}`)
	}
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "lampas", Kind: "tutor", V: v, Talk: talk}, Input: json.RawMessage(`{"question":"hi"}`),
		GrindOverride: override,
	})
	ct, _ := (&apptest.FakeCipher{From: sender}).Encrypt(millKey, string(plain))
	fresh := apptest.NewFakePostern()
	fresh.AddRecord(application.PosternRecord{Txid: "direct:g4", Class: application.GristClass, From: sender, To: millKey,
		Signer: sender, SignerApps: []string{"lampas"}, Ciphertext: ct})
	mill.Postern = fresh
	return mill, fresh, grinder
}

// A new version of a kind main has is proved on its branch: the grind file an
// eval brings lists the version, and the mill takes it, with main's model and
// effort.
func TestGristGrindANewVersionOfAKindMainHasIsGroundWithTheGrindAnEvalBrings(t *testing.T) {
	for _, talk := range []string{"eval:greets:ab12cd34", "smoke:tutor/greets:ab12cd34"} {
		mill, backend, grinder := newVersionMill(t, "factory", talk, "1.2", []string{"1.1", "1.2"})
		runs := &apptest.FakeGristRuns{}
		mill.Runs = runs
		if _, err := mill.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		answer := openFakeAnswer(t, backend.Delivered()[0])
		if answer.Status != application.GristAnswered || answer.Reason == application.GristReasonVersion {
			t.Fatalf("%s: expected the new version ground, got %+v", talk, answer)
		}
		calls := grinder.Calls()
		if len(calls) != 1 || calls[0].Call.Model != "sonnet" || calls[0].Call.Effort != "low" || !strings.HasSuffix(calls[0].Call.System, "SAY REF") {
			t.Fatalf("%s: expected one grind on main's model and effort with the brought text, got %+v", talk, calls)
		}
	}
}

// The ref's versions are taken from the factory's test key in an eval or smoke
// talk alone, and a version neither file lists is refused.
func TestGristGrindANewVersionIsRefusedUnlessTheTestKeyBringsItInAnEvalTalk(t *testing.T) {
	for name, tc := range map[string]struct {
		from, talk, v, reason string
		refVersions           []string
	}{
		"an app's key":                {"phone", "eval:greets:ab12cd34", "1.2", application.GristReasonOverride, []string{"1.1", "1.2"}},
		"the test key, ordinary talk": {"factory", "chat:1", "1.2", application.GristReasonOverride, []string{"1.1", "1.2"}},
		"the test key, no talk":       {"factory", "", "1.2", application.GristReasonOverride, []string{"1.1", "1.2"}},
		"a version in neither file":   {"factory", "eval:greets:ab12cd34", "1.3", application.GristReasonVersion, []string{"1.1", "1.2"}},
		"no grind file brought":       {"factory", "eval:greets:ab12cd34", "1.2", application.GristReasonVersion, nil},
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder := newVersionMill(t, tc.from, tc.talk, tc.v, tc.refVersions)
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != tc.reason {
				t.Errorf("expected the grist refused with %q, got %+v", tc.reason, answer)
			}
			if len(grinder.Calls()) != 0 {
				t.Errorf("expected nothing ground, got %d grinds", len(grinder.Calls()))
			}
		})
	}
}

// millSender is both ends of the wire for an eval: each send is sealed and
// posted by the real GristSend as the factory's test key, put before the real
// mill as the backend would, ground in one pass of it, and its answer brought
// back as GristSend reports one.
type millSender struct {
	t     *testing.T
	mill  application.GristGrind
	inbox *apptest.FakePostern
	sent  []application.GristSendRequest
}

func (m *millSender) Run(ctx context.Context, req application.GristSendRequest) (application.GristSendReport, error) {
	m.t.Helper()
	m.sent = append(m.sent, req)
	millKey, _, _ := (&fakeMillKey{made: true}).PublicKey()
	outbox := apptest.NewFakePostern()
	outbox.Mine = application.PosternMe{Mill: millKey}
	send := application.GristSend{Postern: outbox, Cipher: &apptest.FakeCipher{From: blobFactoryKey}, Keys: &fakeMillKey{made: true}}
	wait := req.Wait
	req.Wait = 0
	if _, err := send.Run(ctx, req); err != nil {
		return application.GristSendReport{}, err
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(outbox.Delivered()[0], &payload); err != nil {
		m.t.Fatal(err)
	}
	for _, blob := range outbox.Uploaded() {
		m.inbox.SetBlob(sha(blob), blob)
	}
	txid, before := "direct:e"+string(rune('0'+len(m.sent))), len(m.inbox.Delivered())
	m.inbox.AddRecord(application.PosternRecord{Txid: txid, Class: application.GristClass, From: blobFactoryKey, To: millKey,
		Signer: blobFactoryKey, SignerApps: []string{"lampas"}, Ciphertext: payload.Ct})
	if _, err := m.mill.Run(ctx); err != nil {
		m.t.Fatal(err)
	}
	delivered := m.inbox.Delivered()
	if len(delivered) != before+1 {
		m.t.Fatalf("expected the mill to answer %s, got %d answers more", txid, len(delivered)-before)
	}
	answer := openFakeAnswer(m.t, delivered[before])
	report := application.GristSendReport{Txid: txid, Answer: &answer}
	if answer.Status != application.GristAnswered {
		return report, &application.GristUnanswered{Txid: txid, Answer: &answer, Wait: wait}
	}
	return report, nil
}

// The schemas of the versioned rig: 1.1's answer is an answer, 1.2's an answer
// and its source.
const (
	answerSchema11 = `{"type": "object", "required": ["answer"], "properties": {"answer": {"type": "string"}}}`
	answerSchema12 = `{"type": "object", "required": ["answer", "source"], "properties": {"answer": {"type": "string"}, "source": {"type": "string"}}}`
)

// versionedRig is a lampas rig whose schemas are named by version, with the real
// send and the real mill behind its eval. Main's tutor grind takes version 1.1,
// with schemas/tutor/1.1/input.json and schemas/tutor/1.1/answer.json; the
// branch mw/story adds version 1.2 with the schemas of that name, the grind file
// naming 1.2's answer schema and more written into it (fields as JSON), and the
// example asks, which is a request of version 1.2. The session answers answer.
func versionedRig(t *testing.T, answer string, more ...string) (application.GristSmoke, *millSender, *apptest.FakeGrinder, *apptest.FakeGrinds) {
	t.Helper()
	const checkout, commit, refCommit = "/rigs/lampas", "c1", "c2"
	grind := func(versions, schema string, more ...string) string {
		fields := append([]string{`"grind": 1, "app": "lampas", "kind": "tutor", "versions": ` + versions,
			`"model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "` + schema + `"`}, more...)
		return "{" + strings.Join(fields, ", ") + "}"
	}
	grinds := apptest.NewFakeGrinds()
	for path, text := range map[string]string{
		"grinds/tutor.json":             grind(`["1.1"]`, "schemas/tutor/1.1/answer.json"),
		"grinds/tutor.md":               "SAY MAIN",
		"schemas/tutor/1.1/input.json":  `{"type": "object", "required": ["question"]}`,
		"schemas/tutor/1.1/answer.json": answerSchema11,
	} {
		grinds.SetFile(checkout, commit, path, []byte(text))
		grinds.SetRefFile(checkout, "mw/story", refCommit, path, []byte(text))
	}
	for path, text := range map[string]string{
		"grinds/tutor.json":               grind(`["1.1", "1.2"]`, "schemas/tutor/1.2/answer.json", more...),
		"grinds/tutor.md":                 "SAY REF",
		"schemas/tutor/1.2/input.json":    `{"type": "object", "required": ["question", "passage"]}`,
		"schemas/tutor/1.2/answer.json":   answerSchema12,
		"grinds/examples/tutor/asks.json": `{"schemaVersion": "1.2", "request": {"question": "hi", "passage": "John 3"}, "expect": {"answer": {"matches": "ref"}}}`,
	} {
		grinds.SetRefFile(checkout, "mw/story", refCommit, path, []byte(text))
	}
	grinder := &apptest.FakeGrinder{Result: application.SessionResult{Subtype: "success", Answer: json.RawMessage(answer)}}
	inbox := apptest.NewFakePostern()
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	sender := &millSender{t: t, inbox: inbox, mill: application.GristGrind{
		Postern: inbox, Cipher: &apptest.FakeCipher{From: millKey}, Keys: keys, TestKey: blobFactoryKey,
		State: apptest.NewFakeGristState(), Grinds: grinds, Grinder: grinder,
		Pass: &apptest.FakeGristLock{}, Grinding: &apptest.FakeGristLock{}, Host: "laptop",
		Apps: map[string]string{"lampas": checkout}, TempDir: t.TempDir(),
		Credits: apptest.UsableCredits(),
		Now:     func() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) },
	}}
	return application.GristSmoke{
		Grinds: grinds, Send: sender, Apps: map[string]string{"lampas": checkout}, TempDir: t.TempDir(),
		Sleep: func(context.Context, time.Duration) error { return nil },
	}, sender, grinder, grinds
}

// compacted is JSON text as the mill gives it to a session.
func compacted(t *testing.T, text string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(text)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// A grind whose input and answer schemas are named by version, end to end: an
// --ref eval of an example of the version the ref adds sends that version's
// answer schema, as the ref's grind file names it, and the mill's session is
// given it, with the ref's instructions and the example's own request
// (mw-gq6.1109).
func TestEvalWithARefSendsAndGrindsWithTheRefsSchemaOfTheExamplesVersion(t *testing.T) {
	smoke, sender, grinder, _ := versionedRig(t, `{"answer": "ref", "source": "John 3"}`)
	var out bytes.Buffer
	if err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story", Out: &out}).Run(context.Background(), "lampas", "tutor", []string{"asks"}); err != nil {
		t.Fatalf("expected the example of version 1.2 to pass at the ref: %v\n%s", err, out.String())
	}
	if len(sender.sent) != 1 || sender.sent[0].Version != "1.2" || sender.sent[0].GrindOverride == nil ||
		sender.sent[0].GrindOverride.Schema != answerSchema12 || sender.sent[0].GrindOverride.Instructions != "SAY REF" {
		t.Fatalf("expected one send of version 1.2 bringing the ref's 1.2 answer schema and instructions, got %+v", sender.sent)
	}
	calls := grinder.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected one grind, got %d", len(calls))
	}
	call := calls[0].Call
	if call.Schema != compacted(t, answerSchema12) || !strings.HasSuffix(call.System, "SAY REF") {
		t.Errorf("expected the session given the ref's 1.2 answer schema and instructions, got schema %s and system %q", call.Schema, call.System)
	}
	if !strings.Contains(call.Prompt, `"passage":"John 3"`) {
		t.Errorf("expected the session given the example's request of version 1.2, got %q", call.Prompt)
	}
}

// The answer of that eval is held to the ref's schema of the version, by the
// mill and by the eval: one that fits only main's 1.1 schema does not pass.
func TestEvalWithARefHoldsTheAnswerToTheRefsSchemaOfTheExamplesVersion(t *testing.T) {
	smoke, _, grinder, _ := versionedRig(t, `{"answer": "ref"}`)
	var out bytes.Buffer
	err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story", Out: &out}).Run(context.Background(), "lampas", "tutor", []string{"asks"})
	if !errors.Is(err, application.ErrGristEvalFailed) {
		t.Fatalf("expected an answer with no source to fail the example of version 1.2, got %v\n%s", err, out.String())
	}
	if len(grinder.Calls()) == 0 {
		t.Errorf("expected the grist ground and its answer refused, not the grist refused unground\n%s", out.String())
	}
}

// What the ref's grind file changes besides its versions is proved by the eval
// too, end to end: an example with a photo passes at a ref whose grind takes a
// photo, in a session cut at the ref's timeout and turns, though main's grind
// takes no photo and sets neither (mw-gq6.1108).
func TestEvalWithARefIsJudgedByTheRefsGrindFile(t *testing.T) {
	smoke, sender, grinder, grinds := versionedRig(t, `{"answer": "ref", "source": "John 3"}`,
		`"attachments": {"min": 1, "max": 1, "mime": ["image/png"]}`, `"timeoutSeconds": 45`, `"maxTurns": 2`)
	grinds.SetRefFile("/rigs/lampas", "mw/story", "c2", "grinds/examples/tutor/shows.json",
		[]byte(`{"schemaVersion": "1.2", "request": {"question": "what is this?", "passage": "John 3"}, "photos": ["page.png"], "expect": {"answer": {"matches": "ref"}}}`))
	grinds.SetRefFile("/rigs/lampas", "mw/story", "c2", "grinds/examples/tutor/page.png", []byte("png bytes"))
	var out bytes.Buffer
	if err := (application.GristEvalExamples{Smoke: smoke, Ref: "mw/story", Out: &out}).Run(context.Background(), "lampas", "tutor", []string{"shows"}); err != nil {
		t.Fatalf("expected the example with a photo to pass at the ref, whose grind takes one: %v\n%s", err, out.String())
	}
	calls := grinder.Calls()
	if len(sender.sent) != 1 || len(calls) != 1 {
		t.Fatalf("expected one send and one grind, got %d and %d", len(sender.sent), len(calls))
	}
	if got := calls[0]; len(got.Files) != 1 || got.Call.Timeout != 45*time.Second || got.Call.MaxTurns != 2 || got.Call.Model != "sonnet" {
		t.Errorf("expected the session given the photo, cut at the ref's 45s and 2 turns, on main's model, got %d files, %s, %d turns and %s",
			len(got.Files), got.Call.Timeout, got.Call.MaxTurns, got.Call.Model)
	}
}

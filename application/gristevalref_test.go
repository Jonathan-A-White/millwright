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
		Now: func() time.Time { return time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC) },
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

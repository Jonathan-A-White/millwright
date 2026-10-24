package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// backendRefusal is an error as the postern client's: the backend answered
// outside 2xx, and its status can be read from the error.
type backendRefusal struct {
	code int
	said string
}

func (e *backendRefusal) Error() string      { return e.said }
func (e *backendRefusal) PosternStatus() int { return e.code }

func refusedWith(code int, body string) error {
	return fmt.Errorf("mw grist send: %w", &backendRefusal{code: code,
		said: fmt.Sprintf("the postern backend at http://10.88.0.2:8787 said %d to GET /api/me: %s", code, body)})
}

// smokeScript is a sender that answers each send with the next error in its
// list; a nil entry is an answer that fits the example.
type smokeScript struct {
	errs []error
	sent int
}

func (s *smokeScript) Run(_ context.Context, _ application.GristSendRequest) (application.GristSendReport, error) {
	s.sent++
	if len(s.errs) == 0 {
		return application.GristSendReport{}, fmt.Errorf("no send was scripted")
	}
	next := s.errs[0]
	s.errs = s.errs[1:]
	if next != nil {
		return application.GristSendReport{}, next
	}
	return application.GristSendReport{Txid: "direct:smoke", Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(`{"price": null, "confidence": "low"}`)}}, nil
}

func smokeWith(sender application.GristSmokeSender) (application.GristSmoke, *[]time.Duration) {
	grinds := apptest.NewFakeGrinds()
	const checkout, commit = "/rigs/lampas", "c1"
	grinds.SetFile(checkout, commit, "grinds/tutor.json", []byte(`{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "schemas/tutor.json"}`))
	grinds.SetFile(checkout, commit, "schemas/tutor.json", []byte(`{"type": "object", "required": ["price", "confidence"], "properties": {"price": {"type": ["number", "null"]}, "confidence": {"enum": ["low", "medium", "high"]}}}`))
	grinds.SetFile(checkout, commit, "grinds/examples/tutor/hello.json", []byte(`{"schemaVersion": "1", "request": {"note": "hi"}, "expect": {"confidence": "low"}}`))
	var waits []time.Duration
	return application.GristSmoke{
		Grinds: grinds, Send: sender, Apps: map[string]string{"lampas": checkout}, TempDir: "",
		Sleep: func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil },
	}, &waits
}

// mw-gq6.540: a send the backend refuses with 429, 502, 503 or 504 is the
// backend's fault: sent once more after a wait, and if that passes, so does the
// example.
func TestABackendRefusalOfASendIsRetriedOnceAndPassesWhenTheRetryDoes(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			sender := &smokeScript{errs: []error{refusedWith(code, "checking licence: WhatsOnChain said 429"), nil}}
			smoke, waits := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 2 || len(*waits) != 1 {
				t.Errorf("expected two sends with one wait, got %d sends, waits %v", sender.sent, *waits)
			}
			if report.Failed() || len(report.NotRun) != 0 {
				t.Errorf("expected the example to pass, got %+v", report)
			}
		})
	}
}

// If the retry is refused too, the example is not run: said, and holding nothing.
func TestABackendRefusalThatRepeatsIsNotRunAndHoldsNothing(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			again := refusedWith(code, "checking licence: WhatsOnChain said 429")
			sender := &smokeScript{errs: []error{again, again}}
			smoke, _ := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 2 {
				t.Errorf("expected the send retried once, got %d sends", sender.sent)
			}
			if report.Failed() || report.Held() {
				t.Errorf("expected nothing failed or held, got %+v", report)
			}
			if len(report.NotRun) != 1 || !strings.Contains(report.NotRun[0], fmt.Sprintf("refused it with %d", code)) {
				t.Errorf("expected the line to say the backend refused it with %d, got %v", code, report.NotRun)
			}
			if !strings.Contains(report.Line(), "not run") {
				t.Errorf("expected a not-run line, got %q", report.Line())
			}
		})
	}
}

// A refusal about the request itself still fails the example, at once.
func TestARefusalAboutTheRequestStillFailsTheExample(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			sender := &smokeScript{errs: []error{refusedWith(code, "no"), nil}}
			smoke, waits := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 1 || len(*waits) != 0 {
				t.Errorf("expected one send and no wait, got %d sends, waits %v", sender.sent, *waits)
			}
			if !report.Failed() || !report.Held() || len(report.NotRun) != 0 {
				t.Errorf("expected a failure that holds, got %+v", report)
			}
			if !strings.Contains(report.Failures[0], "it could not be sent") {
				t.Errorf("expected the old words, got %v", report.Failures)
			}
		})
	}
}

// A backend that cannot be reached at all stays not run, as it was.
func TestAnUnreachableBackendStaysNotRun(t *testing.T) {
	sender := &smokeScript{errs: []error{fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || len(report.NotRun) != 1 || sender.sent != 1 {
		t.Errorf("expected one send, not run, got %d sends, %+v", sender.sent, report)
	}
}

// A 500 whose body names an upstream 429 is the backend's too.
func TestAServerErrorNamingAnUpstream429IsTheBackendsFault(t *testing.T) {
	again := refusedWith(500, "checking licence: WhatsOnChain said 429")
	sender := &smokeScript{errs: []error{again, again}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || len(report.NotRun) != 1 || sender.sent != 2 {
		t.Errorf("expected two sends and not run, got %d sends, %+v", sender.sent, report)
	}
}

// mw-gq6.690: a grind whose session did not finish is said with the words the
// mill kept of why in its record of the run, beside the reason the sender is
// given, which names no cause.
func TestASmokeFailureOfASessionThatDidNotFinishSaysWhyFromTheMillsRecord(t *testing.T) {
	sender := &smokeScript{errs: []error{
		&application.GristUnanswered{Txid: "direct:one", Answer: &application.GristAnswer{Status: application.GristFailed, Reason: application.GristReasonSessionEnd}},
		&application.GristUnanswered{Txid: "direct:two", Answer: &application.GristAnswer{Status: application.GristFailed, Reason: application.GristReasonSessionEnd}},
	}}
	smoke, _ := smokeWith(sender)
	state := apptest.NewFakeGristState()
	smoke.Record = state
	for _, txid := range []string{"direct:one", "direct:two"} {
		line := application.GrindLine{Txid: txid, Status: application.GristFailed, Reason: application.GristReasonSessionEnd,
			Notes: []string{"the harness could not finish the grind of " + txid + ": claude: fork/exec /usr/bin/claude: argument list too long"}}
		if err := state.Append(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0], application.GristReasonSessionEnd) || !strings.Contains(report.Failures[0], "argument list too long") {
		t.Fatalf("expected the failure to say why beside the reason, got %q", report.Failures)
	}
}

// mw-gq6.690: a grind whose instructions (a file, or the parts of a directory
// joined) pass 100,000 bytes is a warning of the smoke, naming app and kind;
// one under it says nothing.
func TestASmokeWarnsOfInstructionsOverAHundredThousandBytes(t *testing.T) {
	for name, tc := range map[string]struct {
		parts map[string]string
		warns bool
	}{
		"one big file":        {map[string]string{"grinds/tutor.md": strings.Repeat("x", 100_001)}, true},
		"a small file":        {map[string]string{"grinds/tutor.md": strings.Repeat("x", 99_000)}, false},
		"many parts together": {map[string]string{"grinds/tutor.instructions/a.md": strings.Repeat("x", 60_000), "grinds/tutor.instructions/b.md": strings.Repeat("y", 60_000)}, true},
	} {
		t.Run(name, func(t *testing.T) {
			smoke, _ := smokeWith(&smokeScript{errs: []error{nil}})
			grinds := smoke.Grinds.(*apptest.FakeGrinds)
			file := "grinds/tutor.md"
			if _, many := tc.parts["grinds/tutor.instructions/a.md"]; many {
				file = "grinds/tutor.instructions"
			}
			grinds.SetFile("/rigs/lampas", "c1", "grinds/tutor.json", []byte(`{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "`+file+`", "answerSchema": "schemas/tutor.json"}`))
			for p, text := range tc.parts {
				grinds.SetFile("/rigs/lampas", "c1", p, []byte(text))
			}
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Join(report.Warnings, "\n")
			if tc.warns != (strings.Contains(got, "lampas/tutor") && strings.Contains(got, "instructions are")) {
				t.Fatalf("expected a warning: %v, got warnings %q", tc.warns, got)
			}
			if report.Failed() {
				t.Fatalf("expected no failure, got %+v", report)
			}
		})
	}
}

// smokeMill is a sender that stands for the mill: each send is answered with
// the next answer in its list, and before the first it does what moved says,
// as a mill that refreshes its own checkout of the rig before each grind.
type smokeMill struct {
	answers []application.GristAnswer
	moved   func()
	sent    int
}

func (m *smokeMill) Run(_ context.Context, _ application.GristSendRequest) (application.GristSendReport, error) {
	if m.sent == 0 && m.moved != nil {
		m.moved()
	}
	m.sent++
	if len(m.answers) == 0 {
		return application.GristSendReport{}, fmt.Errorf("no answer was scripted for send %d", m.sent)
	}
	next := m.answers[0]
	m.answers = m.answers[1:]
	return application.GristSendReport{Txid: fmt.Sprintf("direct:smoke-%d", m.sent), Answer: &next}, nil
}

// withoutProperty is the schema text with one root property taken out: the
// answer schema as it was before a landing added the property.
func withoutProperty(t *testing.T, schema []byte, name string) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(schema, &root); err != nil {
		t.Fatal(err)
	}
	props, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema has no properties")
	}
	if _, there := props[name]; !there {
		t.Fatalf("the schema does not list %s", name)
	}
	delete(props, name)
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// mw-gq6.739: the smoke read lampas's grinds at a87ab771 and ran for most of
// an hour; lampas main moved to f6b9fb9d meanwhile, whose bible-talk schema
// lists fetch and whose instructions ask for it, and the mill, which reads the
// grind afresh for each grist, ground verse-outside-the-held-range there. The
// smoke judged that answer by the schema it had read and failed the example:
// "answer has fetch, which the schema does not allow". An answer the mill says
// it ground at another commit is held to the schema at that commit, read from
// the rig (fetched when the checkout has not seen it yet), and the smoke says
// so in a warning; the examples after it are still read at the smoke's commit.
func TestAnAnswerGroundAtAnotherCommitIsHeldToTheSchemaThere(t *testing.T) {
	schemaNow, err := os.ReadFile("testdata/grind-schemas/lampas-bible-talk.answer.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	schemaBefore := withoutProperty(t, schemaNow, "fetch")
	const checkout, before, after = "/rigs/lampas", "a87ab771", "f6b9fb9d"
	grind := []byte(`{"grind": 1, "app": "lampas", "kind": "bible-talk", "versions": ["1"], "model": "sonnet", "effort": "medium", "instructions": "grinds/bible-talk.instructions", "answerSchema": "grinds/bible-talk.answer.schema.json"}`)
	verseOutside := []byte(`{"schemaVersion": "1", "request": {"reference": "Revelation 2", "verses": "2:1-3", "question": "What does the Lord say to the church in Smyrna, further down this chapter?", "history": []},
		"expect": {"answer": {"matches": "[Vv]erses? 1(-| to | through |–)3|1.{0,3}3"}, "settings_changes": {"present": false}, "setting_offer": {"present": false}}}`)
	workNotTicked := []byte(`{"schemaVersion": "1", "request": {"reference": "John 1", "verses": "1:1", "question": "What does the Word mean here?", "history": []}, "expect": {"answer": {"matches": "Word"}}}`)
	grinds := apptest.NewFakeGrinds()
	grinds.SetFile(checkout, before, "grinds/bible-talk.json", grind)
	grinds.SetFile(checkout, before, "grinds/bible-talk.instructions", []byte("You are the tutor."))
	grinds.SetFile(checkout, before, "grinds/bible-talk.answer.schema.json", schemaBefore)
	grinds.SetFile(checkout, before, "grinds/examples/bible-talk/verse-outside-the-held-range.json", verseOutside)
	grinds.SetFile(checkout, before, "grinds/examples/bible-talk/work-not-ticked.json", workNotTicked)
	mill := &smokeMill{
		moved: func() {
			// lampas main moves under the run: the fetch field lands.
			grinds.SetRemoteFile(checkout, after, "grinds/bible-talk.json", grind)
			grinds.SetRemoteFile(checkout, after, "grinds/bible-talk.instructions", []byte("You are the tutor. Other passages: name them in fetch."))
			grinds.SetRemoteFile(checkout, after, "grinds/bible-talk.answer.schema.json", schemaNow)
			grinds.SetRemoteFile(checkout, after, "grinds/examples/bible-talk/verse-outside-the-held-range.json", verseOutside)
			grinds.SetRemoteFile(checkout, after, "grinds/examples/bible-talk/work-not-ticked.json", workNotTicked)
		},
		answers: []application.GristAnswer{
			{Status: application.GristAnswered, Grind: application.GristAnswerGrind{App: "lampas", Kind: "bible-talk", V: "1", Commit: after},
				Answer: json.RawMessage(`{"answer": "I hold verses 1-3 of Revelation 2 here; the letter to Smyrna begins at verse 8.", "question": "What does the Lord say to the church in Smyrna, further down this chapter?", "words": [], "fetch": ["Revelation 2:8-11"]}`)},
			{Status: application.GristAnswered, Grind: application.GristAnswerGrind{App: "lampas", Kind: "bible-talk", V: "1", Commit: after},
				Answer: json.RawMessage(`{"answer": "The Word is the Son, with God from the beginning.", "question": "What does the Word mean here?", "words": []}`)},
		},
	}
	smoke := application.GristSmoke{Grinds: grinds, Send: mill, Apps: map[string]string{"lampas": checkout},
		Sleep: func(context.Context, time.Duration) error { return nil }}
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Commit != before {
		t.Fatalf("the smoke read the grinds at %s, want %s", report.Commit, before)
	}
	if len(report.Failures) != 0 {
		t.Fatalf("the smoke failed:\n%s", strings.Join(report.Failures, "\n"))
	}
	if report.Examples != 2 || mill.sent != 2 {
		t.Fatalf("sent %d, %d examples; want both sent once", mill.sent, report.Examples)
	}
	warnings := strings.Join(report.Warnings, "\n")
	for _, want := range []string{"lampas/bible-talk/verse-outside-the-held-range", "schema at " + after, before, "moved"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("the warnings do not say %q:\n%s", want, warnings)
		}
	}
	if len(report.Warnings) != 1 {
		t.Errorf("want one warning, got %d:\n%s", len(report.Warnings), warnings)
	}
}

// The schema at the mill's commit is the one the answer is held to even when
// the answer fits neither: the failure then names the mill's commit, not the
// smoke's, so the Mayor reads the right schema.
func TestAnAnswerThatFitsTheSchemaAtNeitherCommitFailsAgainstTheMills(t *testing.T) {
	schemaNow, err := os.ReadFile("testdata/grind-schemas/lampas-bible-talk.answer.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	const checkout, before, after = "/rigs/lampas", "a87ab771", "f6b9fb9d"
	grind := []byte(`{"grind": 1, "app": "lampas", "kind": "bible-talk", "versions": ["1"], "model": "sonnet", "effort": "medium", "instructions": "grinds/bible-talk.instructions", "answerSchema": "grinds/bible-talk.answer.schema.json"}`)
	example := []byte(`{"schemaVersion": "1", "request": {"reference": "Revelation 2", "verses": "2:1-3", "question": "Smyrna?", "history": []}, "expect": {"answer": {"matches": "verses"}}}`)
	grinds := apptest.NewFakeGrinds()
	grinds.SetFile(checkout, before, "grinds/bible-talk.json", grind)
	grinds.SetFile(checkout, before, "grinds/bible-talk.instructions", []byte("You are the tutor."))
	grinds.SetFile(checkout, before, "grinds/bible-talk.answer.schema.json", withoutProperty(t, schemaNow, "fetch"))
	grinds.SetFile(checkout, before, "grinds/examples/bible-talk/verse-outside-the-held-range.json", example)
	wrong := application.GristAnswer{Status: application.GristAnswered, Grind: application.GristAnswerGrind{App: "lampas", Kind: "bible-talk", V: "1", Commit: after},
		Answer: json.RawMessage(`{"answer": "I hold verses 1-3.", "question": "Smyrna?", "words": [], "fetch": ["Revelation 2:8-11"], "footnote": "none"}`)}
	mill := &smokeMill{
		moved: func() {
			grinds.SetRemoteFile(checkout, after, "grinds/bible-talk.json", grind)
			grinds.SetRemoteFile(checkout, after, "grinds/bible-talk.instructions", []byte("You are the tutor."))
			grinds.SetRemoteFile(checkout, after, "grinds/bible-talk.answer.schema.json", schemaNow)
			grinds.SetRemoteFile(checkout, after, "grinds/examples/bible-talk/verse-outside-the-held-range.json", example)
		},
		answers: []application.GristAnswer{wrong, wrong, wrong},
	}
	smoke := application.GristSmoke{Grinds: grinds, Send: mill, Apps: map[string]string{"lampas": checkout},
		Sleep: func(context.Context, time.Duration) error { return nil }}
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 1 {
		t.Fatalf("want one failure, got %q", report.Failures)
	}
	failure := report.Failures[0]
	if strings.Contains(failure, "has fetch") || !strings.Contains(failure, "has footnote, which the schema does not allow") {
		t.Errorf("the failure should be footnote's alone, got %q", failure)
	}
	if !strings.Contains(failure, "schema at "+after) {
		t.Errorf("the failure should name the mill's commit %s, got %q", after, failure)
	}
}

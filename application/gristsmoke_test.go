package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
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
			// Refused twice, then sent once more at the run's end and refused twice again.
			sender := &smokeScript{errs: []error{again, again, again, again}}
			smoke, _ := smokeWith(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if sender.sent != 4 {
				t.Errorf("expected the send retried once and the example sent again at the end, got %d sends", sender.sent)
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
	gone := fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)
	sender := &smokeScript{errs: []error{gone, gone}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || len(report.NotRun) != 1 || sender.sent != 2 {
		t.Errorf("expected one send, not run, got %d sends, %+v", sender.sent, report)
	}
}

// A 500 whose body names an upstream 429 is the backend's too.
func TestAServerErrorNamingAnUpstream429IsTheBackendsFault(t *testing.T) {
	again := refusedWith(500, "checking licence: WhatsOnChain said 429")
	sender := &smokeScript{errs: []error{again, again, again, again}}
	smoke, _ := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || len(report.NotRun) != 1 || sender.sent != 4 {
		t.Errorf("expected four sends and not run, got %d sends, %+v", sender.sent, report)
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

// smokeOfTwo is smokeWith with a second example of the same grind, so that one
// example can fail for its own reasons and the other not be run. The examples go
// in name order: "a-real" first, "hello" second.
func smokeOfTwo(sender application.GristSmokeSender) (application.GristSmoke, *[]time.Duration) {
	smoke, waits := smokeWith(sender)
	grinds := smoke.Grinds.(*apptest.FakeGrinds)
	grinds.SetFile("/rigs/lampas", "c1", "grinds/examples/tutor/a-real.json", []byte(`{"schemaVersion": "1", "request": {"note": "hi"}, "expect": {"confidence": "low"}}`))
	return smoke, waits
}

// mw-6ww.169: an example the backend could not be reached for is not run, and
// stays not run beside an example that really failed: it is no failure, and the
// hold is the real failure's alone.
func TestAnExampleNotRunForTheBackendIsNoFailureBesideARealFailure(t *testing.T) {
	gone := fmt.Errorf("mw grist send: sent as direct:x, but reading its answer failed: asking the postern backend for a challenge: %w",
		fmt.Errorf("reaching the postern backend at http://10.88.0.2:8787: dial tcp 10.88.0.2:8787: connect: connection refused: %w", application.ErrPosternUnreachable))
	sender := &smokeScript{errs: []error{fmt.Errorf("boom"), gone, gone}}
	smoke, _ := smokeOfTwo(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0], "lampas/tutor/a-real: it could not be sent: boom") {
		t.Errorf("expected the one real failure, got %v", report.Failures)
	}
	if len(report.NotRun) != 1 || !strings.Contains(report.NotRun[0], "could not be reached") {
		t.Errorf("expected the unreachable backend as not run, got %v", report.NotRun)
	}
	if !report.Held() {
		t.Errorf("expected the real failure to hold, got %+v", report)
	}
	line := report.Line()
	if !strings.Contains(line, "FAILED 1 of 2 examples") || !strings.Contains(line, "1 example not run") {
		t.Errorf("expected FAILED 1 of 2 and the count not run, got %q", line)
	}
}

// A send or answer read cut off by the network in a way the adapter did not
// classify (a refused connection, a timeout) is the backend out of reach too.
func TestANetworkErrorOfASendIsTheBackendOutOfReach(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}
	for name, cause := range map[string]error{"refused": refused, "timeout": timedOut{}} {
		t.Run(name, func(t *testing.T) {
			err := fmt.Errorf("mw grist send: sent as direct:x, but reading its answer failed: %w", cause)
			sender := &smokeScript{errs: []error{err, err}}
			smoke, _ := smokeWith(sender)
			report, runErr := smoke.Run(context.Background(), "lampas", "")
			if runErr != nil {
				t.Fatal(runErr)
			}
			if report.Failed() || report.Held() || len(report.NotRun) != 1 {
				t.Errorf("expected not run and nothing held, got %+v", report)
			}
		})
	}
}

// timedOut is a net.Error that timed out.
type timedOut struct{}

func (timedOut) Error() string   { return "i/o timeout" }
func (timedOut) Timeout() bool   { return true }
func (timedOut) Temporary() bool { return true }

// Sweep: every other reason the mill could not be asked is not run beside a real
// failure too, per example, and is not sent again.
func TestEveryOtherNotRunReasonStaysNotRunBesideARealFailure(t *testing.T) {
	until := time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)
	cases := map[string]*application.GristUnanswered{
		"the key's daily limit":  {Txid: "direct:b", Answer: &application.GristAnswer{Status: application.GristRefused, Reason: application.GristReasonDaily(100)}},
		"the app's licence":      {Txid: "direct:b", Answer: &application.GristAnswer{Status: application.GristRefused, Reason: application.GristReasonLicence}},
		"the usage limit":        {Txid: "direct:b", Answer: &application.GristAnswer{Status: application.GristFailed, Reason: application.GristReasonLimited(application.HarnessStop{Reason: application.ReasonUsageLimit, Until: until})}},
		"the model's rate limit": {Txid: "direct:b", Answer: &application.GristAnswer{Status: application.GristFailed, Reason: application.GristReasonLimited(application.HarnessStop{Reason: application.ReasonRateLimit, Until: until})}},
		"the model overloaded":   {Txid: "direct:b", Answer: &application.GristAnswer{Status: application.GristFailed, Reason: application.GristReasonLimited(application.HarnessStop{Reason: application.ReasonOverloaded, Until: until})}},
	}
	for name, unanswered := range cases {
		t.Run(name, func(t *testing.T) {
			sender := &smokeScript{errs: []error{fmt.Errorf("boom"), unanswered}}
			smoke, _ := smokeOfTwo(sender)
			report, err := smoke.Run(context.Background(), "lampas", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Failures) != 1 || len(report.NotRun) != 1 || sender.sent != 2 {
				t.Errorf("expected one failure, one not run and two sends, got %d sends %+v", sender.sent, report)
			}
			if !report.Held() {
				t.Errorf("expected the real failure to hold, got %+v", report)
			}
		})
	}
}

// mw-6ww.169: an example the backend could not be reached for is sent once more
// after 60 s, and passes when the backend is back: nothing is not run, nothing
// fails.
func TestAnExampleNotRunForTheBackendIsSentOnceMoreAfterSixtySeconds(t *testing.T) {
	gone := fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)
	sender := &smokeScript{errs: []error{gone, nil}}
	smoke, waits := smokeWith(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if sender.sent != 2 || len(*waits) != 1 || (*waits)[0] != application.GristSmokeRetryWait || application.GristSmokeRetryWait != 60*time.Second {
		t.Errorf("expected two sends with one wait of 60s, got %d sends, waits %v", sender.sent, *waits)
	}
	if report.Failed() || len(report.NotRun) != 0 || len(report.Runs) != 0 || len(report.Failures) != 0 {
		t.Errorf("expected the example to pass on the second try, got %+v", report)
	}
}

// The retry of one example that fails for its own reason fails it, as a send
// would; one still out of reach is not run, with the count in the mail's line.
func TestTheRetryAtTheEndKeepsEachExamplesOwnVerdict(t *testing.T) {
	gone := fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)
	// a-real and hello are both out of reach; on the retry a-real is answered
	// with a refusal about the request and hello is out of reach still.
	sender := &smokeScript{errs: []error{gone, gone, refusedWith(403, "no"), gone}}
	smoke, _ := smokeOfTwo(sender)
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if sender.sent != 4 {
		t.Errorf("expected each example sent twice, got %d", sender.sent)
	}
	if len(report.Failures) != 1 || !strings.Contains(report.Failures[0], "lampas/tutor/a-real: it could not be sent") {
		t.Errorf("expected a-real to fail on its retry, got %v", report.Failures)
	}
	if len(report.NotRun) != 1 || report.Runs["lampas/tutor/hello"].NotRun != true || report.Runs["lampas/tutor/a-real"].NotRun {
		t.Errorf("expected hello alone to stay not run, got %+v", report)
	}
	if line := report.Line(); !strings.Contains(line, "1 example not run") || !strings.Contains(line, "FAILED 1 of 2") {
		t.Errorf("expected the line to say FAILED 1 of 2 and 1 example not run, got %q", line)
	}
}

// A run of nothing but not-run examples says their count too.
func TestANotRunLineSaysHowManyExamplesWereNotRun(t *testing.T) {
	gone := fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)
	smoke, _ := smokeOfTwo(&smokeScript{errs: []error{gone, gone, gone, gone}})
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || !strings.Contains(report.Line(), "2 examples not run") {
		t.Errorf("expected not run with the count 2, got %q", report.Line())
	}
}

// swapScript is a swap watch that says a swap is running for the first n looks.
type swapScript struct {
	running int
	looks   int
}

func (w *swapScript) SwapRunning(context.Context) (bool, error) {
	w.looks++
	return w.looks <= w.running, nil
}

// A smoke run while the backend is being swapped waits for the swap to finish
// before its first send.
func TestASmokeWaitsForABackendSwapToFinishBeforeItSends(t *testing.T) {
	sender := &smokeScript{errs: []error{nil}}
	smoke, waits := smokeWith(sender)
	smoke.Swap = &swapScript{running: 3}
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*waits) != 3 || sender.sent != 1 || report.Failed() {
		t.Errorf("expected three waits for the swap and one send, got waits %v, %d sends, %+v", *waits, sender.sent, report)
	}
}

// A swap that does not finish is waited for five minutes, then the smoke sends.
func TestASmokeWaitsAtMostFiveMinutesForABackendSwap(t *testing.T) {
	sender := &smokeScript{errs: []error{nil}}
	smoke, waits := smokeWith(sender)
	smoke.Swap = &swapScript{running: 1 << 30}
	if _, err := smoke.Run(context.Background(), "lampas", ""); err != nil {
		t.Fatal(err)
	}
	var total time.Duration
	for _, d := range *waits {
		total += d
	}
	if total != application.GristSmokeSwapWait || application.GristSmokeSwapWait != 5*time.Minute || sender.sent != 1 {
		t.Errorf("expected waits of 5 minutes in all and a send, got %v (%d sends)", total, sender.sent)
	}
}

// BackendSwapWatch reads a swap that has started and not reported back from the
// hands notes; a finished one, another step, and a stale start are not.
func TestBackendSwapWatchSeesAStartedSwapStep(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 45, 0, 0, time.UTC)
	started := func(ago time.Duration, exit int, why string) string {
		raw, _ := json.Marshal(application.HandsRan{At: now.Add(-ago).Format(time.RFC3339), Exit: exit, Host: "home", Why: why})
		return string(raw)
	}
	for name, test := range map[string]struct {
		key, value string
		want       bool
	}{
		"a swap just started":           {"hands.ran.mw-mr4xgrw.59.backend-abc123", started(time.Minute, -1, application.HandsStartedWhy), true},
		"a swap that finished":          {"hands.ran.mw-mr4xgrw.59.backend-abc123", started(time.Minute, 0, ""), false},
		"a start nobody reported on":    {"hands.ran.mw-mr4xgrw.59.backend-abc123", started(time.Hour, -1, application.HandsStartedWhy), false},
		"another step that just starts": {"hands.ran.mw-5r3p30.1.deploy-site", started(time.Minute, -1, application.HandsStartedWhy), false},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := apptest.NewFakeTracker()
			if err := tracker.SetNote(context.Background(), test.key, test.value); err != nil {
				t.Fatal(err)
			}
			running, err := application.BackendSwapWatch{Notes: tracker, Now: func() time.Time { return now }}.SwapRunning(context.Background())
			if err != nil || running != test.want {
				t.Errorf("expected %v, got %v %v", test.want, running, err)
			}
		})
	}
}

// A failure that holds is recorded with the examples not run beside it, and the
// record says them in mw status and in its line; a new failure is recorded over
// a standing one (the not-run examples do not stop it).
func TestAFailedRecordSaysTheExamplesNotRunBesideIt(t *testing.T) {
	ctx := context.Background()
	gone := fmt.Errorf("reaching it: %w", application.ErrPosternUnreachable)
	smoke, _ := smokeOfTwo(&smokeScript{errs: []error{fmt.Errorf("boom"), gone, gone}})
	report, err := smoke.Run(ctx, "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	tracker := apptest.NewFakeTracker()
	book := application.GristSmokeBook{Notes: tracker, Host: "laptop"}
	// An earlier failure stands; the new smoke's failure replaces it.
	if err := book.Record(ctx, "millwright", application.GristSmokeReport{App: "lampas", Examples: 1, Failures: []string{"lampas/tutor/old: boom"}}); err != nil {
		t.Fatal(err)
	}
	if err := book.Record(ctx, "millwright", report); err != nil {
		t.Fatal(err)
	}
	records, err := book.Records(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("%v %v", records, err)
	}
	if got := records[0].FailingExamples(); len(got) != 1 || got[0] != "lampas/tutor/a-real" {
		t.Errorf("expected the new failure to be the record, got %v", records[0].Failures)
	}
	if line := records[0].Line(); !strings.Contains(line, "also not run, and held for nothing") || !strings.Contains(line, "1 example not run") {
		t.Errorf("expected the record's line to say what was not run, got %q", line)
	}
}

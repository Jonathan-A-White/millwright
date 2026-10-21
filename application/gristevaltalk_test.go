package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// talkSent is one send a talkSender saw: its talk, and the request it read.
type talkSent struct {
	talk, request string
	photos        []string
}

// talkSender answers each send with the next scripted answer (or a plain
// {"answer": "ok"}) and keeps what it was sent.
type talkSender struct {
	sent    []talkSent
	answers []string
}

func (s *talkSender) Run(_ context.Context, req application.GristSendRequest) (application.GristSendReport, error) {
	raw, err := os.ReadFile(req.RequestFile)
	if err != nil {
		return application.GristSendReport{}, err
	}
	var photos []string
	for _, p := range req.Photos {
		photos = append(photos, p[strings.LastIndex(p, "/")+1:])
	}
	s.sent = append(s.sent, talkSent{talk: req.Talk, request: string(raw), photos: photos})
	answer := `{"answer": "ok"}`
	if len(s.answers) > 0 {
		answer, s.answers = s.answers[0], s.answers[1:]
	}
	return application.GristSendReport{Txid: "direct:t", Answer: &application.GristAnswer{
		Status: application.GristAnswered, Answer: json.RawMessage(answer),
		Talk: &application.GristAnswerTalk{Session: application.GristTalkFresh, Turns: len(s.sent)},
	}}, nil
}

// talkEnder is a GristTalkEnder that keeps what it was asked.
type talkEnder struct {
	ended  []string
	tidied []string
	err    error
}

func (e *talkEnder) End(_ context.Context, app, talk string) (int, error) {
	e.ended = append(e.ended, app+" "+talk)
	return 1, e.err
}

func (e *talkEnder) Tidy(_ context.Context, app string) (int, error) {
	e.tidied = append(e.tidied, app)
	return 0, nil
}

const twoTurnExample = `{
  "schemaVersion": "1",
  "request": {"question": "my son is Luke", "history": []},
  "turn_2": {"request": {"question": "who is my son?", "history": [{"q": "a", "a": "b"}]}},
  "expect": {"answer": {"matches": "Luke"}}
}`

func talkSmoke(t *testing.T, sender application.GristSmokeSender, example string) application.GristSmoke {
	t.Helper()
	grinds := apptest.NewFakeGrinds()
	const checkout, commit = "/rigs/lampas", "c1"
	grinds.SetFile(checkout, commit, "grinds/tutor.json", []byte(`{"grind": 1, "app": "lampas", "kind": "tutor", "versions": ["1"], "model": "sonnet", "effort": "low", "instructions": "grinds/tutor.md", "answerSchema": "schemas/tutor.json"}`))
	grinds.SetFile(checkout, commit, "schemas/tutor.json", []byte(`{"type": "object", "required": ["answer"], "properties": {"answer": {"type": "string"}}}`))
	grinds.SetFile(checkout, commit, "grinds/examples/tutor/remembers.json", []byte(example))
	return application.GristSmoke{
		Grinds: grinds, Send: sender, Apps: map[string]string{"lampas": checkout}, TempDir: t.TempDir(),
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
}

func TestATwoTurnExampleIsReadWithItsSecondTurn(t *testing.T) {
	example, err := application.ParseGristExample([]byte(twoTurnExample))
	if err != nil {
		t.Fatal(err)
	}
	if example.Turn2 == nil || !strings.Contains(string(example.Turn2.Request), "who is my son") {
		t.Fatalf("expected the second turn's request, got %+v", example.Turn2)
	}
	for name, doc := range map[string]string{
		"a second turn with no request":       `{"schemaVersion": "1", "request": {"q": 1}, "turn_2": {}}`,
		"a second turn of another version":    `{"schemaVersion": "1", "request": {"q": 1}, "turn_2": {"request": {"schemaVersion": "2"}}}`,
		"a second turn of a misspelt check":   `{"schemaVersion": "1", "request": {"q": 1}, "turn_2": {"request": {"q": 2}}, "expect": {"a": {"mathces": "x"}}}`,
		"a second turn naming another's file": `{"schemaVersion": "1", "request": {"q": 1}, "turn_2": {"request": {"q": 2}, "photos": ["../x.jpg"]}}`,
	} {
		if _, err := application.ParseGristExample([]byte(doc)); err == nil {
			t.Errorf("%s: expected the example to be refused", name)
		}
	}
	if one, err := application.ParseGristExample([]byte(`{"schemaVersion": "1", "request": {"q": 1}}`)); err != nil || one.Turn2 != nil {
		t.Errorf("a one-turn example reads as it did: %+v, %v", one, err)
	}
}

func TestTheSmokeSendsBothTurnsOfAnExampleUnderOneTalkAndEndsIt(t *testing.T) {
	sender := &talkSender{answers: []string{`{"answer": "noted"}`, `{"answer": "your son is Luke"}`}}
	ender := &talkEnder{}
	smoke := talkSmoke(t, sender, twoTurnExample)
	smoke.Talks = ender
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() || report.Examples != 1 {
		t.Fatalf("expected the example to pass, got %+v", report)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("expected two sends, got %d", len(sender.sent))
	}
	talk := sender.sent[0].talk
	if !regexp.MustCompile(`^smoke:tutor/remembers:[0-9a-f]{8}$`).MatchString(talk) || sender.sent[1].talk != talk {
		t.Errorf("expected both turns under one smoke: talk, got %q and %q", talk, sender.sent[1].talk)
	}
	if !strings.Contains(sender.sent[0].request, `"my son is Luke"`) || !strings.Contains(sender.sent[1].request, `"who is my son?"`) ||
		strings.Contains(sender.sent[1].request, "history") {
		t.Errorf("expected turn 1's request, then turn 2's with no history: %+v", sender.sent)
	}
	if want := []string{"lampas " + talk}; !slices.Equal(ender.ended, want) {
		t.Errorf("expected the talk ended once, got %v, want %v", ender.ended, want)
	}
	if !slices.Equal(ender.tidied, []string{"lampas"}) {
		t.Errorf("expected the app's old smoke and eval talks tidied once, got %v", ender.tidied)
	}
}

func TestOnlyTheSecondTurnIsHeldToTheExpectation(t *testing.T) {
	// Turn 1's answer does not say Luke and need not: it only has to fit the schema.
	sender := &talkSender{answers: []string{`{"answer": "noted"}`, `{"answer": "I do not remember"}`}}
	ender := &talkEnder{}
	smoke := talkSmoke(t, sender, twoTurnExample)
	smoke.Talks = ender
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Failed() || !strings.Contains(report.Failures[0], "turn 2") || !strings.Contains(report.Failures[0], "answer") {
		t.Fatalf("expected a failure naming turn 2 and the field, got %+v", report)
	}
	if len(ender.ended) == 0 {
		t.Error("a failed example ends its talk too")
	}
	// A wrong answer is sent again, a whole talk each time: three talks, each ended.
	if len(sender.sent) != 6 || len(ender.ended) != 3 || ender.ended[0] == ender.ended[1] {
		t.Errorf("expected 3 talks of 2 turns, each ended, got %d sends and %v", len(sender.sent), ender.ended)
	}
}

func TestATalkIsEndedWhateverBecameOfIt(t *testing.T) {
	ender := &talkEnder{err: errors.New("the store is locked")}
	var said bytes.Buffer
	smoke := talkSmoke(t, &talkSender{answers: []string{`{"answer": "x"}`, `{"answer": "Luke"}`}}, twoTurnExample)
	smoke.Talks, smoke.Out = ender, &said
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil || report.Failed() {
		t.Fatalf("a talk that cannot be ended does not fail the example: %+v, %v", report, err)
	}
	if !strings.Contains(said.String(), "the store is locked") {
		t.Errorf("expected the failure to end it said, got %q", said.String())
	}
}

func TestAOneTurnExampleSendsNoTalk(t *testing.T) {
	sender := &talkSender{}
	ender := &talkEnder{}
	smoke := talkSmoke(t, sender, `{"schemaVersion": "1", "request": {"question": "hi"}, "expect": {"answer": "ok"}}`)
	smoke.Talks = ender
	report, err := smoke.Run(context.Background(), "lampas", "")
	if err != nil || report.Failed() {
		t.Fatalf("%+v, %v", report, err)
	}
	if len(sender.sent) != 1 || sender.sent[0].talk != "" || len(ender.ended) != 0 {
		t.Errorf("expected one send with no talk and nothing ended, got %+v, %v", sender.sent, ender.ended)
	}
}

func TestATalkNameFitsTheLimitWhateverTheExampleIsCalled(t *testing.T) {
	long := strings.Repeat("long-name-", 12)
	sender := &talkSender{}
	smoke := talkSmoke(t, sender, twoTurnExample)
	grinds := smoke.Grinds.(*apptest.FakeGrinds)
	grinds.SetFile("/rigs/lampas", "c1", "grinds/examples/tutor/"+long+".json", []byte(twoTurnExample))
	smoke.Talks = &talkEnder{}
	if _, err := smoke.Run(context.Background(), "lampas", ""); err != nil {
		t.Fatal(err)
	}
	for _, s := range sender.sent {
		if len(s.talk) > application.GristTalkMax || !strings.HasPrefix(s.talk, "smoke:tutor/") {
			t.Errorf("talk %q (%d bytes) is not a smoke talk of at most %d", s.talk, len(s.talk), application.GristTalkMax)
		}
	}
}

func TestEvalOfAnExampleSendsAsEvalAndPrintsTheSecondAnswer(t *testing.T) {
	sender := &talkSender{answers: []string{`{"answer": "noted"}`, `{"answer": "your son is Luke"}`}}
	ender := &talkEnder{}
	smoke := talkSmoke(t, sender, twoTurnExample)
	smoke.Talks = ender
	var out bytes.Buffer
	err := application.GristEvalExamples{Smoke: smoke, Out: &out}.Run(context.Background(), "lampas", "tutor", []string{"remembers"})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !regexp.MustCompile(`^eval:remembers:[0-9a-f]{8}$`).MatchString(sender.sent[0].talk) || sender.sent[1].talk != sender.sent[0].talk {
		t.Errorf("expected both turns under one eval: talk, got %q and %q", sender.sent[0].talk, sender.sent[1].talk)
	}
	for _, want := range []string{"lampas/tutor/remembers", "ok", "your son is Luke"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected the report to say %q, got:\n%s", want, out.String())
		}
	}
	if !slices.Equal(ender.ended, []string{"lampas " + sender.sent[0].talk}) || !slices.Equal(ender.tidied, []string{"lampas"}) {
		t.Errorf("expected the talk ended and the old ones tidied, got %v and %v", ender.ended, ender.tidied)
	}
}

func TestEvalOfAnExampleThatMissesFailsAndNamesTheField(t *testing.T) {
	smoke := talkSmoke(t, &talkSender{answers: []string{`{"answer": "x"}`, `{"answer": "no idea"}`}}, twoTurnExample)
	var out bytes.Buffer
	err := application.GristEvalExamples{Smoke: smoke, Out: &out}.Run(context.Background(), "lampas", "tutor", []string{"remembers"})
	if !errors.Is(err, application.ErrGristEvalFailed) {
		t.Fatalf("expected the eval to fail, got %v", err)
	}
	if !strings.Contains(out.String(), "FAILED") || !strings.Contains(out.String(), "answer") || strings.Count(out.String(), "sending") != 1 {
		t.Errorf("expected one send, a failure and the field, got:\n%s", out.String())
	}
}

func TestEvalOfAnExampleThatIsNotThereIsRefused(t *testing.T) {
	smoke := talkSmoke(t, &talkSender{}, twoTurnExample)
	err := application.GristEvalExamples{Smoke: smoke}.Run(context.Background(), "lampas", "tutor", []string{"forgets"})
	if err == nil || !strings.Contains(err.Error(), "remembers") {
		t.Errorf("expected the refusal to name the examples there are, got %v", err)
	}
	if err := (application.GristEvalExamples{Smoke: smoke}).Run(context.Background(), "lampas", "nosuch", nil); err == nil {
		t.Error("expected a kind with no grind to be refused")
	}
}

func TestEvalWithNoExampleNamedRunsEveryExampleOfTheKind(t *testing.T) {
	sender := &talkSender{}
	smoke := talkSmoke(t, sender, twoTurnExample)
	smoke.Grinds.(*apptest.FakeGrinds).SetFile("/rigs/lampas", "c1", "grinds/examples/tutor/plain.json", []byte(`{"schemaVersion": "1", "request": {"question": "hi"}}`))
	sender.answers = []string{`{"answer": "a"}`, `{"answer": "b"}`, `{"answer": "Luke"}`}
	var out bytes.Buffer
	if err := (application.GristEvalExamples{Smoke: smoke, Out: &out}).Run(context.Background(), "lampas", "tutor", nil); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(sender.sent) != 3 {
		t.Errorf("expected 3 sends (a plain example and a two-turn one), got %d", len(sender.sent))
	}
}

// memTalks is a GristTalkStore in memory, with the transcripts it keeps.
type memTalks struct {
	records     map[application.GristTalkKey]application.GristTalk
	transcripts map[string]bool
}

func newMemTalks() *memTalks {
	return &memTalks{records: map[application.GristTalkKey]application.GristTalk{}, transcripts: map[string]bool{}}
}

func (m *memTalks) Cwd(app string) (string, error) { return "/state/cwd/" + app, nil }
func (m *memTalks) Load(_ context.Context, key application.GristTalkKey) (application.GristTalk, bool, error) {
	talk, ok := m.records[key]
	return talk, ok, nil
}
func (m *memTalks) Save(_ context.Context, talk application.GristTalk) error {
	m.records[talk.GristTalkKey] = talk
	return nil
}
func (m *memTalks) Remove(_ context.Context, key application.GristTalkKey) error {
	delete(m.records, key)
	return nil
}
func (m *memTalks) List(context.Context) ([]application.GristTalk, error) {
	var all []application.GristTalk
	for _, talk := range m.records {
		all = append(all, talk)
	}
	slices.SortFunc(all, func(a, b application.GristTalk) int { return strings.Compare(a.Talk, b.Talk) })
	return all, nil
}
func (m *memTalks) Transcript(_, session string) (bool, error) { return m.transcripts[session], nil }
func (m *memTalks) RemoveTranscript(_, session string) error {
	delete(m.transcripts, session)
	return nil
}

func (m *memTalks) put(app, sender, talk, session string, ago time.Duration, now time.Time, ended ...string) {
	rec := application.GristTalk{
		GristTalkKey: application.GristTalkKey{App: app, Sender: sender, Talk: talk},
		Session:      session, Cwd: "/state/cwd/" + app, Turns: 1, LastUsed: now.Add(-ago), Ended: ended,
	}
	m.records[rec.GristTalkKey] = rec
	m.transcripts[session] = true
	for _, e := range ended {
		m.transcripts[e] = true
	}
}

func (m *memTalks) names() []string {
	var names []string
	for key := range m.records {
		names = append(names, key.Talk)
	}
	slices.Sort(names)
	return names
}

func TestTheSweepEndsATalkAndLeavesNoRecordAndNoTranscript(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	store := newMemTalks()
	store.put("lampas", "key-a", "eval:remembers:0000aaaa", "s-1", time.Minute, now, "s-0")
	store.put("lampas", "key-b", "eval:remembers:0000aaaa", "s-2", time.Minute, now)
	store.put("lampas", "key-a", "rom.8@1", "s-3", time.Minute, now)
	ended, err := application.GristTalkSweep{Store: store, Now: func() time.Time { return now }}.End(context.Background(), "lampas", "eval:remembers:0000aaaa")
	if err != nil || ended != 2 {
		t.Fatalf("expected both senders' records ended, got %d, %v", ended, err)
	}
	if !slices.Equal(store.names(), []string{"rom.8@1"}) {
		t.Errorf("expected only the app's own talk left, got %v", store.names())
	}
	if len(store.transcripts) != 1 || !store.transcripts["s-3"] {
		t.Errorf("expected the ended talks' transcripts gone (an earlier session's too) and the other's kept, got %v", store.transcripts)
	}
	// A talk there is no record of is no error: the mill may not be on this host.
	if n, err := (application.GristTalkSweep{Store: store}).End(context.Background(), "lampas", "eval:gone:00000000"); err != nil || n != 0 {
		t.Errorf("expected nothing to end and no error, got %d, %v", n, err)
	}
}

func TestTheSweepTidiesOldEvalAndSmokeTalksOnly(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	store := newMemTalks()
	store.put("lampas", "k", "eval:old:11111111", "s-1", 90*time.Minute, now)
	store.put("lampas", "k", "smoke:tutor/old:22222222", "s-2", 2*time.Hour, now)
	store.put("lampas", "k", "eval:young:33333333", "s-3", 10*time.Minute, now)
	store.put("lampas", "k", "rom.8@1", "s-4", 5*time.Hour, now)
	store.put("cairn", "k", "eval:old:44444444", "s-5", 5*time.Hour, now)
	ended, err := application.GristTalkSweep{Store: store, Now: func() time.Time { return now }}.Tidy(context.Background(), "lampas")
	if err != nil || ended != 2 {
		t.Fatalf("expected 2 old talks tidied, got %d, %v", ended, err)
	}
	if want := []string{"eval:old:44444444", "eval:young:33333333", "rom.8@1"}; !slices.Equal(store.names(), want) {
		t.Errorf("expected %v left, got %v", want, store.names())
	}
	if store.transcripts["s-1"] || store.transcripts["s-2"] || !store.transcripts["s-3"] || !store.transcripts["s-4"] || !store.transcripts["s-5"] {
		t.Errorf("expected only the two tidied talks' transcripts gone, got %v", store.transcripts)
	}
}

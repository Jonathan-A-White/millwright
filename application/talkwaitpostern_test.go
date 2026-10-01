package application_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const (
	talkWaitMayorKey    = "mayor-pubkey-hex"
	talkWaitGovernorKey = "governor-pubkey-hex"
)

// talkWaitStream is a PosternStream that says hello at a head of 0 and then
// announces one indexed record, like the backend does.
type talkWaitStream struct{ seq int64 }

func (s talkWaitStream) Events(_ context.Context, onEvent func(application.PosternEvent) error) error {
	if err := onEvent(application.PosternEvent{Kind: application.PosternEventHello}); err != nil {
		return err
	}
	return onEvent(application.PosternEvent{Kind: application.PosternEventMessage, Seq: s.seq})
}

// talkPosternFixture is a fake postern holding records for the Mayor's key,
// and a TalkWait over it that sees a hello and then a message event for head.
type talkPosternFixture struct {
	t       *testing.T
	postern *apptest.FakePostern
	tracker *apptest.FakeTracker
	cipher  *apptest.FakeCipher
}

func newTalkPosternFixture(t *testing.T) *talkPosternFixture {
	t.Helper()
	cipher := apptest.NewFakeCipher()
	cipher.From = talkWaitGovernorKey
	return &talkPosternFixture{t: t, postern: apptest.NewFakePostern(), tracker: apptest.NewFakeTracker(), cipher: cipher}
}

// addMessage indexes a plain message record from the Governor to the Mayor, a
// post on the named channel (a topic) unless that is empty, and returns its seq.
func (f *talkPosternFixture) addMessage(txid, topic, text string) int64 {
	f.t.Helper()
	plain := text
	if topic != "" {
		wrapped, _ := json.Marshal(map[string]any{"thread": map[string]any{"topic": topic}, "text": text})
		plain = string(wrapped)
	}
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, plain)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq
}

// addTurn indexes a talk turn from the Governor to the Mayor.
func (f *talkPosternFixture) addTurn(text string) int64 {
	f.t.Helper()
	plain, _ := json.Marshal(map[string]any{
		"talk": map[string]any{"id": "talk-7", "turn": 3}, "text": text, "role": application.TalkRoleTurn,
	})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(plain))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: "direct:turn", Class: "talk", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq
}

// run waits with the stream announcing head, returning the report and what was
// printed. The wait is given a short limit: a run that hears nothing ends on it.
func (f *talkPosternFixture) run(head int64) (application.TalkWaitReport, string) {
	f.t.Helper()
	var out strings.Builder
	report, err := application.TalkWait{
		Stream:      talkWaitStream{seq: head},
		Postern:     f.postern,
		Cipher:      apptest.NewFakeCipher(),
		Keys:        stubPosternKeys{pubKey: talkWaitMayorKey},
		Memory:      f.tracker,
		GovernorKey: talkWaitGovernorKey,
		Limit:       200 * time.Millisecond,
		MinBackoff:  5 * time.Millisecond,
		MaxBackoff:  5 * time.Millisecond,
		Out:         &out,
	}.Run(context.Background())
	if err != nil {
		f.t.Fatalf("the wait: %v", err)
	}
	return report, out.String()
}

// AC1: a new postern message for the seat's key ends the wait and is printed
// with its channel, txid and first line.
func TestTalkWaitEndsOnANewPosternMessageAndPrintsIt(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addMessage("direct:abc123", "", "Look at this screenshot\nand the second line")

	report, printed := f.run(seq)

	if report.Turn != nil {
		t.Errorf("expected no turn, got %+v", report.Turn)
	}
	if len(report.Postern) != 1 || report.Postern[0].Txid != "direct:abc123" {
		t.Fatalf("expected the one message reported, got %+v", report.Postern)
	}
	for _, want := range []string{"new postern message", "channel general", "direct:abc123", "Look at this screenshot"} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected %q in what was printed, got:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "second line") {
		t.Errorf("expected only the first line printed, got:\n%s", printed)
	}
	if strings.Contains(printed, "No talk turn") {
		t.Errorf("expected the wait to end on the message, not its limit, got:\n%s", printed)
	}
}

// A message in a named channel is printed under that channel.
func TestTalkWaitNamesTheChannelOfAPosternMessage(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addMessage("direct:topic1", "Screens", "Here")

	_, printed := f.run(seq)

	if !strings.Contains(printed, `channel "Screens"`) {
		t.Errorf("expected the named channel printed, got:\n%s", printed)
	}
}

// AC2: a message the postern inbox has already read does not wake the wait.
func TestTalkWaitIgnoresAPosternMessageAlreadyRead(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addMessage("direct:old", "", "Already read")
	if err := f.tracker.SetNote(context.Background(), application.PosternCursorKey, strconv.FormatInt(seq, 10)); err != nil {
		t.Fatal(err)
	}

	report, printed := f.run(seq)

	if report.Turn != nil || len(report.Postern) != 0 {
		t.Errorf("expected nothing heard, got turn %+v and messages %+v", report.Turn, report.Postern)
	}
	if !strings.Contains(printed, "No talk turn") || strings.Contains(printed, "new postern message") {
		t.Errorf("expected the wait to end on its limit, saying nothing of the message, got:\n%s", printed)
	}
}

// A message to some other key does not wake the wait.
func TestTalkWaitIgnoresAPosternMessageForAnotherKey(t *testing.T) {
	f := newTalkPosternFixture(t)
	ct, _ := f.cipher.Encrypt("someone-else", "not yours")
	seq := f.postern.AddRecord(application.PosternRecord{
		Txid: "direct:other", Class: "message", To: "someone-else", From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq

	report, printed := f.run(seq)

	if len(report.Postern) != 0 || strings.Contains(printed, "new postern message") {
		t.Errorf("expected nothing heard, got %+v:\n%s", report.Postern, printed)
	}
}

// AC3: when a Governor turn and a postern message both arrive, the turn is what
// the wait ends on, whichever came first, and it is printed first.
func TestTalkWaitPrefersAGovernorTurnWhenBothArrive(t *testing.T) {
	for _, tc := range []struct {
		name        string
		turnFirst   bool
		wantMessage bool
	}{
		{"message then turn", false, true},
		{"turn then message", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newTalkPosternFixture(t)
			var head int64
			if tc.turnFirst {
				f.addTurn("What landed?")
				head = f.addMessage("direct:later", "", "A later message")
			} else {
				f.addMessage("direct:earlier", "", "An earlier message")
				head = f.addTurn("What landed?")
			}

			report, printed := f.run(head)

			if report.Turn == nil || report.Turn.Text != "What landed?" {
				t.Fatalf("expected the turn heard, got %+v", report.Turn)
			}
			if !strings.Contains(printed, "talk talk-7 turn 3 (role turn)") {
				t.Errorf("expected the turn printed, got:\n%s", printed)
			}
			if got := strings.Contains(printed, "new postern message"); got != tc.wantMessage {
				t.Errorf("message reported = %v, want %v:\n%s", got, tc.wantMessage, printed)
			}
			if tc.wantMessage && strings.Index(printed, "new postern message") < strings.Index(printed, "talk talk-7") {
				t.Errorf("expected the turn printed before the message, got:\n%s", printed)
			}
		})
	}
}

// A message that came after a turn is not lost: the next wait hears it.
func TestTalkWaitHearsTheMessageAfterATurnOnTheNextRun(t *testing.T) {
	f := newTalkPosternFixture(t)
	f.addTurn("What landed?")
	head := f.addMessage("direct:later", "", "A later message")

	if report, _ := f.run(head); report.Turn == nil {
		t.Fatal("expected the first wait to hear the turn")
	}
	report, printed := f.run(head)

	if len(report.Postern) != 1 || !strings.Contains(printed, "direct:later") {
		t.Errorf("expected the second wait to hear the later message, got %+v:\n%s", report.Postern, printed)
	}
}

package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const (
	talkHoldMayorKey    = "mayor-pubkey-hex"
	talkHoldGovernorKey = "governor-pubkey-hex"
)

// talkHoldStream is a PosternStream that says hello at a head of 0 and then
// announces one indexed record, like the backend does.
type talkHoldStream struct{ seq int64 }

func (s talkHoldStream) Events(_ context.Context, onEvent func(application.PosternEvent) error) error {
	if err := onEvent(application.PosternEvent{Kind: application.PosternEventHello}); err != nil {
		return err
	}
	return onEvent(application.PosternEvent{Kind: application.PosternEventMessage, Seq: s.seq})
}

// orderedOut notes how many records had been delivered when the wait first
// wrote to it.
type orderedOut struct {
	sb               strings.Builder
	postern          *apptest.FakePostern
	deliveredAtFirst int
	written          bool
}

func (o *orderedOut) Write(p []byte) (int, error) {
	if !o.written {
		o.written, o.deliveredAtFirst = true, len(o.postern.Delivered())
	}
	return o.sb.Write(p)
}

// talkHoldRun runs one TalkWait against a fake postern holding one Governor
// record, with holdingReply as the opt-in text.
func talkHoldRun(t *testing.T, holdingReply, role string) (*apptest.FakePostern, *orderedOut) {
	t.Helper()
	cipher := apptest.NewFakeCipher()
	cipher.From = talkHoldGovernorKey
	plain, _ := json.Marshal(map[string]any{
		"talk": map[string]any{"id": "talk-7", "turn": 3}, "text": "What landed?", "role": role,
	})
	ct, err := cipher.Encrypt(talkHoldMayorKey, string(plain))
	if err != nil {
		t.Fatal(err)
	}
	fake := apptest.NewFakePostern()
	fake.AddRecord(application.PosternRecord{
		Class: "talk", To: talkHoldMayorKey, From: talkHoldGovernorKey, Ciphertext: ct,
	})
	out := &orderedOut{postern: fake}
	_, err = application.TalkWait{
		Stream:       talkHoldStream{seq: 1},
		Postern:      fake,
		Cipher:       apptest.NewFakeCipher(),
		Keys:         stubPosternKeys{pubKey: talkHoldMayorKey},
		Memory:       apptest.NewFakeTracker(),
		GovernorKey:  talkHoldGovernorKey,
		HoldingReply: holdingReply,
		Out:          out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("the wait: %v", err)
	}
	return fake, out
}

func TestTalkWaitSendsTheHoldingReplyToTheTurnBeforeItPrints(t *testing.T) {
	fake, out := talkHoldRun(t, "One moment.", application.TalkRoleTurn)

	delivered := fake.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one holding record, %d were delivered", len(delivered))
	}
	if out.deliveredAtFirst != 1 {
		t.Errorf("expected the holding record delivered before the wait printed, %d had been", out.deliveredAtFirst)
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(delivered[0], &payload); err != nil {
		t.Fatalf("the payload: %v", err)
	}
	if payload.Class != "talk" || payload.To != talkHoldGovernorKey || payload.From != talkHoldMayorKey {
		t.Errorf("expected a talk record from the Mayor to the Governor, got class %q to %q from %q", payload.Class, payload.To, payload.From)
	}
	text, _, err := apptest.NewFakeCipher().Decrypt("priv", payload.Ct)
	if err != nil {
		t.Fatal(err)
	}
	var turn application.TalkTurn
	if err := json.Unmarshal([]byte(text), &turn); err != nil {
		t.Fatal(err)
	}
	if turn.Role != application.TalkRoleHolding || turn.Text != "One moment." || turn.Talk.ID != "talk-7" || turn.Talk.Turn != 3 {
		t.Errorf("expected a holding record saying %q for talk-7 turn 3, got %+v", "One moment.", turn)
	}
	printed := out.sb.String()
	if !strings.Contains(printed, "talk talk-7 turn 3 (role turn)") || !strings.Contains(printed, "What landed?") {
		t.Errorf("expected the turn printed as ever, got:\n%s", printed)
	}
	if !regexp.MustCompile(`(?m)^holding sent in \d+ ms$`).MatchString(printed) {
		t.Errorf("expected a 'holding sent in N ms' line, got:\n%s", printed)
	}
}

func TestTalkWaitSendsNoHoldingReplyWhenNoneIsConfigured(t *testing.T) {
	fake, out := talkHoldRun(t, "", application.TalkRoleTurn)
	if n := len(fake.Delivered()); n != 0 {
		t.Errorf("expected no holding record, %d were delivered", n)
	}
	if strings.Contains(out.sb.String(), "holding sent") {
		t.Errorf("expected no holding line, got:\n%s", out.sb.String())
	}
	if !strings.Contains(out.sb.String(), "talk talk-7 turn 3") {
		t.Errorf("expected the turn printed, got:\n%s", out.sb.String())
	}
}

func TestTalkWaitSendsNoHoldingReplyForTheEndOfATalk(t *testing.T) {
	fake, out := talkHoldRun(t, "One moment.", application.TalkRoleEnd)
	if n := len(fake.Delivered()); n != 0 {
		t.Errorf("expected no holding record for an end, %d were delivered", n)
	}
	if strings.Contains(out.sb.String(), "holding sent") {
		t.Errorf("expected no holding line, got:\n%s", out.sb.String())
	}
	if !strings.Contains(out.sb.String(), "role end") {
		t.Errorf("expected the end printed, got:\n%s", out.sb.String())
	}
}

func TestTalkWaitStillPrintsTheTurnWhenTheHoldingReplyCannotBeSent(t *testing.T) {
	cipher := apptest.NewFakeCipher()
	cipher.From = talkHoldGovernorKey
	plain := `{"talk":{"id":"talk-7","turn":3},"text":"Hi","role":"turn"}`
	ct, _ := cipher.Encrypt(talkHoldMayorKey, plain)
	fake := apptest.NewFakePostern()
	fake.AddRecord(application.PosternRecord{Class: "talk", To: talkHoldMayorKey, From: talkHoldGovernorKey, Ciphertext: ct})
	fake.DeliverErr = fmt.Errorf("the backend is down")
	var out, errOut strings.Builder
	_, err := application.TalkWait{
		Stream: talkHoldStream{seq: 1}, Postern: fake, Cipher: apptest.NewFakeCipher(),
		Keys: stubPosternKeys{pubKey: talkHoldMayorKey}, Memory: apptest.NewFakeTracker(),
		GovernorKey: talkHoldGovernorKey, HoldingReply: "One moment.", Out: &out, Err: &errOut,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("the wait should not fail on a holding reply that would not send: %v", err)
	}
	if !strings.Contains(out.String(), "talk talk-7 turn 3") || strings.Contains(out.String(), "holding sent") {
		t.Errorf("expected the turn and no holding line, got:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "holding") {
		t.Errorf("expected the failure said on stderr, got:\n%s", errOut.String())
	}
}

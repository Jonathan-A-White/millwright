package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// talkSayPlain runs one TalkSay and returns the plaintext of the record it
// delivered, as the Governor reads it.
func talkSayPlain(t *testing.T, req application.TalkSayRequest) string {
	t.Helper()
	fake := apptest.NewFakePostern()
	_, err := application.TalkSay{
		Postern:     fake,
		Cipher:      apptest.NewFakeCipher(),
		Keys:        stubPosternKeys{pubKey: "mayor-pubkey-hex"},
		GovernorKey: "governor-pubkey-hex",
	}.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("talk say: %v", err)
	}
	delivered := fake.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one record, %d were delivered", len(delivered))
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(delivered[0], &payload); err != nil {
		t.Fatal(err)
	}
	plain, _, err := apptest.NewFakeCipher().Decrypt("priv", payload.Ct)
	if err != nil {
		t.Fatal(err)
	}
	return plain
}

func TestTalkSayCarriesLinksInTheRecordNeverInTheText(t *testing.T) {
	plain := talkSayPlain(t, application.TalkSayRequest{
		Text: "Two stories landed.", TalkID: "talk-7", Turn: 3,
		Links: []string{"mw-x.1", "mw-x.2"},
	})
	var turn application.TalkTurn
	if err := json.Unmarshal([]byte(plain), &turn); err != nil {
		t.Fatal(err)
	}
	if len(turn.Links) != 2 || turn.Links[0] != "mw-x.1" || turn.Links[1] != "mw-x.2" {
		t.Errorf("expected links [mw-x.1 mw-x.2], got %v", turn.Links)
	}
	if turn.Text != "Two stories landed." || strings.Contains(turn.Text, "mw-x") {
		t.Errorf("expected the text untouched by the links, got %q", turn.Text)
	}
}

func TestTalkSayWithoutLinksLeavesTheRecordAsItWas(t *testing.T) {
	plain := talkSayPlain(t, application.TalkSayRequest{Text: "Two stories landed.", TalkID: "talk-7", Turn: 3})
	want := `{"talk":{"id":"talk-7","turn":3},"text":"Two stories landed.","role":"answer","model":"","cut":false}`
	if plain != want {
		t.Errorf("expected the plaintext as before\n  %s\ngot\n  %s", want, plain)
	}
}

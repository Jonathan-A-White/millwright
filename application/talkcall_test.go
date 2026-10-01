package application_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// echoKeys signs a record as the hex of its payload, so a test reads back
// what a broadcast carried.
type echoKeys struct{ stubPosternKeys }

func (echoKeys) Sign(_ []application.PosternUtxo, payload []byte) (string, error) {
	return hex.EncodeToString(payload), nil
}

// clearEnvelope is a record's payload as it travels in the clear.
type clearEnvelope struct {
	Class   string `json:"class"`
	Role    string `json:"role"`
	Summary string `json:"summary"`
}

func clearOf(t *testing.T, payload []byte) (clearEnvelope, map[string]json.RawMessage) {
	t.Helper()
	var clear clearEnvelope
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &clear); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	return clear, fields
}

// ring runs one TalkCall over a fake backend that holds a spendable output.
func ring(t *testing.T, req application.TalkCallRequest) *apptest.FakePostern {
	t.Helper()
	backend := apptest.NewFakePostern()
	backend.SetUtxos("", application.PosternUtxo{Txid: strings.Repeat("1", 64), Satoshis: 1000})
	_, err := application.TalkCall{
		Postern: backend, Cipher: apptest.NewFakeCipher(),
		Keys: echoKeys{stubPosternKeys{pubKey: "mayor-pubkey-hex"}}, GovernorKey: "governor-pubkey-hex",
		FloatSats: 100000,
	}.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("talk call: %v", err)
	}
	return backend
}

func TestTalkCallDirectRingCarriesTheRoleAndItsReasonAsSummary(t *testing.T) {
	backend := ring(t, application.TalkCallRequest{Text: "Back now: two landings."})
	delivered := backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one direct record, got %d", len(delivered))
	}
	clear, _ := clearOf(t, delivered[0])
	if clear.Class != "call" || clear.Role != "ring" || clear.Summary != "Back now: two landings." {
		t.Errorf("expected class call, role ring and the reason as summary, got %+v", clear)
	}
}

func TestTalkCallCutsALongReasonToEightyRunes(t *testing.T) {
	reason := strings.Repeat("é", 81)
	backend := ring(t, application.TalkCallRequest{Text: reason})
	clear, _ := clearOf(t, backend.Delivered()[0])
	if n := len([]rune(clear.Summary)); n != 80 {
		t.Errorf("expected a summary of 80 runes, got %d: %q", n, clear.Summary)
	}
	if !strings.HasPrefix(clear.Summary, strings.Repeat("é", 79)) {
		t.Errorf("expected the reason's start kept, got %q", clear.Summary)
	}
}

func TestTalkCallExactlyEightyRunesIsKeptWhole(t *testing.T) {
	reason := strings.Repeat("é", 80)
	backend := ring(t, application.TalkCallRequest{Text: reason})
	if clear, _ := clearOf(t, backend.Delivered()[0]); clear.Summary != reason {
		t.Errorf("expected 80 runes kept whole, got %q", clear.Summary)
	}
}

func TestTalkCallSummaryEndsInNoSpace(t *testing.T) {
	reason := strings.Repeat("a", 78) + " " + strings.Repeat("b", 10)
	backend := ring(t, application.TalkCallRequest{Text: "  " + reason + "  "})
	clear, _ := clearOf(t, backend.Delivered()[0])
	if strings.HasSuffix(clear.Summary, " ") || strings.Contains(clear.Summary, " …") || strings.HasPrefix(clear.Summary, " ") {
		t.Errorf("expected no stray space in the summary, got %q", clear.Summary)
	}
	if len([]rune(clear.Summary)) > 80 {
		t.Errorf("expected at most 80 runes, got %q", clear.Summary)
	}
}

func TestTalkCallChainCopyCarriesTheRoleAndNoSummary(t *testing.T) {
	backend := ring(t, application.TalkCallRequest{Text: "Back now.", Chain: true})
	broadcasts := backend.Broadcasts()
	if len(broadcasts) != 1 {
		t.Fatalf("expected one broadcast, got %d", len(broadcasts))
	}
	payload, err := hex.DecodeString(broadcasts[0])
	if err != nil {
		t.Fatal(err)
	}
	clear, fields := clearOf(t, payload)
	if clear.Class != "call" || clear.Role != "ring" {
		t.Errorf("expected class call and role ring on the chain copy, got %+v", clear)
	}
	if _, has := fields["summary"]; has {
		t.Errorf("expected no summary on the chain copy, got %s", payload)
	}
	if direct, _ := clearOf(t, backend.Delivered()[0]); direct.Summary != "Back now." {
		t.Errorf("expected the direct record to keep its summary, got %+v", direct)
	}
}

// A plain mw postern send leaves the role empty.
func TestPosternSendCarriesNoRole(t *testing.T) {
	f := newSendFixture()
	if _, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "Ready."}); err != nil {
		t.Fatal(err)
	}
	if _, fields := clearOf(t, f.backend.Delivered()[0]); fields["role"] != nil {
		t.Errorf("expected no role on a posted message, got %s", fields["role"])
	}
}

func TestTalkCallAlsoEmitsOneEmergencyEventNamingTheRing(t *testing.T) {
	log := &apptest.FakeEventLog{}
	backend := apptest.NewFakePostern()
	backend.NextTxid = "ring-txid"
	_, err := application.TalkCall{
		Postern: backend, Cipher: apptest.NewFakeCipher(),
		Keys: echoKeys{stubPosternKeys{pubKey: "mayor-pubkey-hex"}}, GovernorKey: "governor-pubkey-hex",
		FloatSats: 100000, Log: log, Actor: "mayor@laptop",
		Now: func() time.Time { return time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC) },
	}.Run(context.Background(), application.TalkCallRequest{Text: "Back now."})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := log.Since(context.Background(), 0)
	if len(got) != 1 {
		t.Fatalf("%d events in the log, want one", len(got))
	}
	if e := got[0]; e.Lane != events.LaneEmergency || e.Kind != events.KindMessage || e.Actor != "mayor@laptop" || e.Detail != "ring-txid" {
		t.Fatalf("the event is %+v, want an emergency message by mayor@laptop whose detail is the ring's txid", e)
	}
}

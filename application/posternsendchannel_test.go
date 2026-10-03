package application_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A direct record names its channel in the clear, postern's docs/protocol.md
// section 1 `channel` / `bead`, so the push's title can say where it is: a
// named channel as "channel", a bead's channel (or a question's bead) as
// "bead"; Factory, which names nothing, neither.
func TestPosternSendNamesTheChannelInTheDirectRecord(t *testing.T) {
	cases := map[string]struct {
		req           application.PosternSendRequest
		channel, bead string
	}{
		"a named channel":  {req: application.PosternSendRequest{Text: "hi", Topic: "general"}, channel: "general"},
		"a bead's channel": {req: application.PosternSendRequest{Text: "hi", Thread: "mw-a.1"}, bead: "mw-a.1"},
		"a question":       {req: application.PosternSendRequest{Class: "decision-needed", Text: "ok?", Bead: "mw-a.1", Options: []string{"a", "b"}}, bead: "mw-a.1"},
		"factory":          {req: application.PosternSendRequest{Text: "hi"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSendFixture()
			if _, err := f.send("").Run(context.Background(), c.req); err != nil {
				t.Fatal(err)
			}
			payload, _ := f.delivered(t, 0)
			if payload.Channel != c.channel || payload.Bead != c.bead {
				t.Fatalf("expected channel %q and bead %q, got %q and %q", c.channel, c.bead, payload.Channel, payload.Bead)
			}
			// Left off, not empty, when there is nothing to name.
			raw := f.backend.Delivered()[0]
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(raw, &keys); err != nil {
				t.Fatal(err)
			}
			if _, ok := keys["channel"]; ok != (c.channel != "") {
				t.Fatalf("channel key presence wrong in %s", raw)
			}
			if _, ok := keys["bead"]; ok != (c.bead != "") {
				t.Fatalf("bead key presence wrong in %s", raw)
			}
		})
	}
}

// The chain copy is public for good, and the Governor has not said a clear
// channel name may be on it: it carries neither field.
func TestPosternSendChainCopyNamesNeitherChannelNorBead(t *testing.T) {
	for name, req := range map[string]application.PosternSendRequest{
		"a named channel":  {Text: "hi", Topic: "general", Chain: true},
		"a bead's channel": {Text: "hi", Thread: "mw-a.1", Chain: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, send := chainReplyFixture()
			if _, err := send.Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if payload, _ := f.delivered(t, 0); payload.Channel == "" && payload.Bead == "" {
				t.Fatal("expected the direct copy to name its channel")
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(chainCopy(t, f, 0), &keys); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"channel", "bead"} {
				if _, ok := keys[key]; ok {
					t.Fatalf("the chain record carries %q: %s", key, chainCopy(t, f, 0))
				}
			}
		})
	}
}

// A send whose channel is the chain channel puts a record on chain alone, so
// it names neither.
func TestPosternSendOnTheChainChannelNamesNeitherChannelNorBead(t *testing.T) {
	f := newSendFixture()
	send := f.send(application.PosternChannelChain)
	send.Keys = echoKeys{stubPosternKeys{pubKey: sendMayorKey}}
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: "hi", Topic: "general"}); err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(chainCopy(t, f, 0), &keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"channel", "bead"} {
		if _, ok := keys[key]; ok {
			t.Fatalf("the chain record carries %q", key)
		}
	}
}

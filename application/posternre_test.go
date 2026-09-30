package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const reTxid = "ab0d0cd9f1e2d3c4b5a697887766554433221100ffeeddccbbaa99887766a8b6"

// A message that answers a General post carries the post's txid as re and no
// thread, in the printed form with its direct: prefix and as a bare txid
// alike; padding around it is dropped.
func TestPosternSendAnswersInGeneralWithRe(t *testing.T) {
	for name, re := range map[string]string{
		"printed form": "direct:" + reTxid,
		"bare txid":    reTxid,
		"padded":       "  direct:" + reTxid + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newSendFixture()
			if _, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "Here are the links.", Re: re}); err != nil {
				t.Fatalf("sending: %v", err)
			}
			want := `{"text":"Here are the links.","re":"` + strings.TrimSpace(re) + `"}`
			if _, text := f.delivered(t, 0); text != want {
				t.Fatalf("expected the General body %s, got %s", want, text)
			}
		})
	}
}

// --re answers inside a post's thread in any channel: it goes with
// --bead-channel and --channel (Thread and Topic), and only --bead, which
// asks a question of its own, conflicts with it.
func TestPosternSendRefusesReWithABeadQuestionOnly(t *testing.T) {
	req := application.PosternSendRequest{Text: "x", Class: "decision-needed", Bead: "mw-a.1", Re: "direct:" + reTxid}
	if err := req.ValidateReplyFlags(); err == nil || !strings.Contains(err.Error(), "--re") || !strings.Contains(err.Error(), "--bead") {
		t.Fatalf("expected a refusal naming --re and --bead, got %v", err)
	}
	for name, req := range map[string]application.PosternSendRequest{
		"general":      {Text: "x", Re: "direct:" + reTxid},
		"bead channel": {Text: "x", Re: "direct:" + reTxid, Thread: "mw-a.1"},
		"channel":      {Text: "x", Re: "direct:" + reTxid, Topic: "garden"},
	} {
		if err := req.ValidateReplyFlags(); err != nil {
			t.Fatalf("%s: expected --re accepted, got %v", name, err)
		}
	}
}

func reInbox(t *testing.T, plaintext string) string {
	t.Helper()
	f := newReleaseTapFixture(t)
	f.cipher.From = releaseTapGovernorKey
	ct, err := f.cipher.Encrypt(releaseTapInboxPubKey, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: "direct:reply", Class: "message", From: releaseTapGovernorKey, To: releaseTapInboxPubKey, Ciphertext: ct,
	})
	var out bytes.Buffer
	inbox := f.inbox()
	inbox.Out = &out
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	return out.String()
}

// A message that answers another prints its re on its line and its text
// unwrapped; one without prints no re.
func TestPosternInboxPrintsReOfAnAnswer(t *testing.T) {
	out := reInbox(t, `{"text":"Thanks, looks right.","re":"direct:`+reTxid+`"}`)
	if !strings.Contains(out, "channel general") || !strings.Contains(out, "re direct:"+reTxid) || !strings.Contains(out, "\nThanks, looks right.\n") {
		t.Fatalf("expected the general thread, its re and the bare text, got:\n%s", out)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "re direct:"+reTxid) {
		t.Fatalf("expected re on the message's own line, got:\n%s", out)
	}
	if strings.Contains(out, `"re"`) {
		t.Fatalf("expected the JSON body unwrapped, got:\n%s", out)
	}

	plain := reInbox(t, "Just saying hi.")
	if first := strings.SplitN(plain, "\n", 2)[0]; strings.Contains(first, " re ") {
		t.Fatalf("expected no re on a plain message, got:\n%s", plain)
	}
	if !strings.Contains(plain, "channel general") {
		t.Fatalf("expected the plain message still printed, got:\n%s", plain)
	}
}

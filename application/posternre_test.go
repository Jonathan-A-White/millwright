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

// --re is an answer in General: it names no bead, so --bead, --thread and
// --topic each conflict with it, and the refusal names both flags.
func TestPosternSendRefusesReWithAnotherThread(t *testing.T) {
	for _, flag := range []string{"--bead", "--thread", "--topic"} {
		t.Run(flag, func(t *testing.T) {
			req := application.PosternSendRequest{Text: "x", Re: "direct:" + reTxid}
			switch flag {
			case "--bead":
				req.Class, req.Bead = "decision-needed", "mw-a.1"
			case "--thread":
				req.Thread = "mw-a.1"
			case "--topic":
				req.Topic = "garden"
			}
			err := req.ValidateReplyFlags()
			if err == nil || !strings.Contains(err.Error(), "--re") || !strings.Contains(err.Error(), flag) {
				t.Fatalf("expected a refusal naming --re and %s, got %v", flag, err)
			}
		})
	}
	if err := (application.PosternSendRequest{Text: "x", Re: "direct:" + reTxid}).ValidateReplyFlags(); err != nil {
		t.Fatalf("expected --re alone accepted, got %v", err)
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
	if !strings.Contains(out, "thread general") || !strings.Contains(out, "re direct:"+reTxid) || !strings.Contains(out, "\nThanks, looks right.\n") {
		t.Fatalf("expected the general thread, its re and the bare text, got:\n%s", out)
	}
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "re direct:"+reTxid) {
		t.Fatalf("expected re on the message's own line, got:\n%s", out)
	}
	if strings.Contains(out, `"re"`) {
		t.Fatalf("expected the JSON body unwrapped, got:\n%s", out)
	}

	plain := reInbox(t, "Just saying hi.")
	if strings.Contains(plain, " re ") || strings.Contains(plain, "re direct") {
		t.Fatalf("expected no re on a plain message, got:\n%s", plain)
	}
	if !strings.Contains(plain, "thread general") {
		t.Fatalf("expected the plain message still printed, got:\n%s", plain)
	}
}

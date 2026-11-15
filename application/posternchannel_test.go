package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// channelInbox reads one message from the Governor, of class message, with
// the given plaintext and txid, printing the inbox as a person reads it.
func channelInbox(t *testing.T, plaintext, txid string) string {
	t.Helper()
	f := newReleaseTapFixture(t)
	f.cipher.From = releaseTapGovernorKey
	ct, err := f.cipher.Encrypt(releaseTapInboxPubKey, plaintext)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: releaseTapInboxPubKey, Signer: releaseTapGovernorKey, Ciphertext: ct,
	})
	var out bytes.Buffer
	inbox := f.inbox()
	inbox.Out = &out
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	return out.String()
}

func threaded(t *testing.T, thread application.PosternThread, re string) string {
	t.Helper()
	raw, err := json.Marshal(application.PosternThreadedMessage{Thread: thread, Text: "hello", Re: re})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// A General post prints its channel and the command that answers inside its
// thread, with its own txid as --re.
func TestPosternInboxGeneralPostPrintsItsChannelAndHowToAnswer(t *testing.T) {
	out := channelInbox(t, "Just saying hi.", "direct:post1")
	if !strings.Contains(out, "channel general") || strings.Contains(out, "thread general") {
		t.Fatalf("expected 'channel general', got:\n%s", out)
	}
	if want := "answer in its thread: mw postern send --re direct:post1 \"...\"\n"; !strings.Contains(out, want) {
		t.Fatalf("expected %q, got:\n%s", want, out)
	}
}

// A reply inside a thread is answered with the post's txid, its own re.
func TestPosternInboxReplyAnswersWithItsRe(t *testing.T) {
	out := channelInbox(t, threaded(t, application.PosternThread{}, "direct:root"), "direct:reply")
	if want := "mw postern send --re direct:root \"...\""; !strings.Contains(out, want) {
		t.Fatalf("expected %q, got:\n%s", want, out)
	}
	if strings.Contains(out, "--re direct:reply") {
		t.Fatalf("expected the reply's own txid not used as --re, got:\n%s", out)
	}
}

// A named channel prints its name quoted and carries --channel in the answer.
func TestPosternInboxNamedChannelAnswerCarriesChannel(t *testing.T) {
	out := channelInbox(t, threaded(t, application.PosternThread{Topic: "roadmap"}, ""), "direct:r1")
	if !strings.Contains(out, `channel "roadmap"`) {
		t.Fatalf("expected the quoted channel, got:\n%s", out)
	}
	if want := `mw postern send --channel "roadmap" --re direct:r1 "..."`; !strings.Contains(out, want) {
		t.Fatalf("expected %q, got:\n%s", want, out)
	}
}

// A bead's channel prints the bead, and its answer carries --bead-channel.
func TestPosternInboxBeadChannelAnswerCarriesBeadChannel(t *testing.T) {
	out := channelInbox(t, threaded(t, application.PosternThread{Bead: "mw-zz.9"}, ""), "direct:b1")
	if !strings.Contains(out, "channel mw-zz.9") {
		t.Fatalf("expected the bead's channel, got:\n%s", out)
	}
	if want := `mw postern send --bead-channel mw-zz.9 --re direct:b1 "..."`; !strings.Contains(out, want) {
		t.Fatalf("expected %q, got:\n%s", want, out)
	}
}

// Only the Governor's (verified) messages get the answer line.
func TestPosternInboxOthersGetNoAnswerLine(t *testing.T) {
	f := newReleaseTapFixture(t)
	f.cipher.From = "someone-else-pubkey-hex"
	ct, err := f.cipher.Encrypt(releaseTapInboxPubKey, "hi")
	if err != nil {
		t.Fatal(err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: "direct:x", Class: "message", From: "someone-else-pubkey-hex", To: releaseTapInboxPubKey, Ciphertext: ct,
	})
	var out bytes.Buffer
	inbox := f.inbox()
	inbox.Out = &out
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "answer in its thread") {
		t.Fatalf("expected no answer line for another sender, got:\n%s", out.String())
	}
}

// The mail about the Governor's message in a bead's channel ends with the
// answer line.
func TestApplyMailOnABeadChannelEndsWithTheAnswerLine(t *testing.T) {
	f := newApplyFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-comment", threaded(t, application.PosternThread{Bead: "mw-e.2"}, ""))

	f.apply(t)

	mail, err := f.mailbox.Inbox(context.Background(), application.MayorMailbox)
	mustDo(t, err)
	if len(mail) != 1 {
		t.Fatalf("expected one mail, got %d", len(mail))
	}
	want := `answer in its thread: mw postern send --bead-channel mw-e.2 --re tx-comment "..."`
	if !strings.HasSuffix(strings.TrimSpace(mail[0].Body), want) {
		t.Fatalf("expected the body to end with %q, got:\n%s", want, mail[0].Body)
	}
}

// --bead without --class decision-needed says where to post in a bead's channel.
func TestPosternSendBeadWithoutClassPointsAtBeadChannel(t *testing.T) {
	f := newSendFixture()
	_, err := f.send("").Run(context.Background(), application.PosternSendRequest{Class: "message", Text: "x", Bead: "mw-a.1"})
	if err == nil || !strings.Contains(err.Error(), "--bead-channel <id> posts in a bead's channel") {
		t.Fatalf("expected the refusal to name --bead-channel, got %v", err)
	}
}

// A reply of his in a bead's or a named channel to a root is answered with
// --re alone: the root may be in Factory, where send refuses a channel flag
// that is not the root's, and mw finds the root's channel from the --re.
func TestPosternInboxReplyAnswerNamesNoChannelFlag(t *testing.T) {
	for name, thread := range map[string]application.PosternThread{
		"bead":  {Bead: "mw-zz.9"},
		"named": {Topic: "roadmap"},
	} {
		t.Run(name, func(t *testing.T) {
			out := channelInbox(t, threaded(t, thread, "direct:factoryroot"), "direct:reply")
			want := `answer in its thread: mw postern send --re direct:factoryroot "..."`
			if !strings.Contains(out, want) {
				t.Fatalf("expected %q, got:\n%s", want, out)
			}
			if strings.Contains(out, "--bead-channel") || strings.Contains(out, "--channel") {
				t.Fatalf("expected no channel flag in the answer line, got:\n%s", out)
			}
		})
	}
}

// A root mw has not seen, answered with --re and no channel flag, is refused
// and nothing is sent: the txid may be a talk turn's, and a reply that fell to
// Factory landed outside the Governor's thread. The refusal names the way to
// answer a talk turn, the flags for another channel and the way to answer a
// root in Factory, which has no channel flag of its own (mw-gq6.1161,
// mw-gq6.1327).
func TestPosternSendReOfAnUnseenRootIsRefusedAndSendsNothing(t *testing.T) {
	f := newSendFixture()
	send := f.send("direct")
	send.Threads = apptest.NewFakePosternThreadIndex()

	_, err := send.Run(context.Background(), application.PosternSendRequest{Class: "message", Text: "Yes.", Re: "direct:unseen"})
	if err == nil {
		t.Fatalf("expected the reply to an unseen root to be refused, it was sent")
	}
	for _, say := range []string{"direct:unseen", "not a post mw knows", "mw talk say --talk <id> --turn <n>", "--bead-channel <id>", "--channel <name>", "--factory"} {
		if !strings.Contains(err.Error(), say) {
			t.Errorf("expected the refusal to say %q, got: %v", say, err)
		}
	}
	if n := len(f.backend.Delivered()); n != 0 {
		t.Fatalf("expected nothing sent, %d were", n)
	}
}

// --factory says the root is in Factory, the one channel with no flag to name:
// the reply to a root mw has not seen is then sent there, with no note.
func TestPosternSendReWithFactoryAnswersAnUnseenRootInFactory(t *testing.T) {
	f := newSendFixture()
	send := f.send("direct")
	send.Threads = apptest.NewFakePosternThreadIndex()

	_, err := send.Run(context.Background(), application.PosternSendRequest{Class: "message", Text: "Yes.", Re: "direct:unseen", InFactory: true})
	if err != nil {
		t.Fatalf("expected the reply to be sent, got %v", err)
	}
	_, plaintext := f.delivered(t, 0)
	want := `{"text":"Yes.","re":"direct:unseen"}`
	if plaintext != want {
		t.Fatalf("expected the plaintext %s, got %s", want, plaintext)
	}
}

// --factory is for a root in Factory: a root mw knows to be in another channel
// is not answered in Factory because it was given.
func TestPosternSendFactoryFlagDoesNotMoveAKnownRoot(t *testing.T) {
	f := newSendFixture()
	send := f.send("direct")
	send.Threads = apptest.NewFakePosternThreadIndex()
	if err := send.Threads.Remember("direct:root", application.PosternThread{Topic: "roadmap"}); err != nil {
		t.Fatal(err)
	}
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Class: "message", Text: "x", Re: "direct:root", InFactory: true}); err != nil {
		t.Fatalf("expected the reply to be sent, got %v", err)
	}
	_, plaintext := f.delivered(t, 0)
	if want := `{"thread":{"topic":"roadmap"},"text":"x","re":"direct:root"}`; plaintext != want {
		t.Fatalf("expected %s, got %s", want, plaintext)
	}
}

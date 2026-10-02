package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// promptFixture is the apply fixture with a backend that keeps /top5, which
// takes a --duration, and an inbox that can answer the Governor.
type promptFixture struct {
	*applyFixture
	prompts *apptest.FakePrompts
}

func newPromptFixture(t *testing.T) *promptFixture {
	t.Helper()
	prompts := apptest.NewFakePrompts()
	mustDo(t, prompts.Put(context.Background(), domain.Prompt{
		Name: "top5", Summary: "the five that matter", Signature: []string{"duration:string=30m"}, Body: "Top five for <duration>.",
	}))
	mustDo(t, prompts.Put(context.Background(), domain.Prompt{
		Name: "wrapup", Summary: "end of day", Signature: []string{}, Body: "Wrap up.",
	}))
	mustDo(t, prompts.Put(context.Background(), domain.Prompt{
		Name: "later", Summary: "park a want", Signature: []string{"text:text:required"}, Body: "Park <text>.",
	}))
	return &promptFixture{applyFixture: newApplyFixture(t), prompts: prompts}
}

func (f *promptFixture) inbox() application.PosternInbox {
	inbox := f.applyFixture.inbox()
	inbox.Prompts = f.prompts
	inbox.Sender = &application.PosternSend{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: applyInboxKey},
		GovernorKey: releaseTapGovernorKey,
	}
	return inbox
}

func (f *promptFixture) apply(t *testing.T) []string {
	t.Helper()
	lines, err := f.inbox().Apply(context.Background())
	if err != nil {
		t.Fatalf("applying: %v", err)
	}
	return lines
}

// answered is every message delivered back to the Governor, decrypted.
func (f *promptFixture) answered(t *testing.T) []application.PosternThreadedMessage {
	t.Helper()
	var out []application.PosternThreadedMessage
	for _, d := range f.backend.Delivered() {
		var payload application.PosternPayload
		mustDo(t, json.Unmarshal(d, &payload))
		plain, _, err := f.cipher.Decrypt("governor", payload.Ct)
		mustDo(t, err)
		var msg application.PosternThreadedMessage
		if json.Unmarshal([]byte(plain), &msg) != nil || msg.Text == "" {
			msg = application.PosternThreadedMessage{Text: plain}
		}
		out = append(out, msg)
	}
	return out
}

// A good call is printed as a call, mailed as "Prompt: ...", and the Mayor
// is not also told "Governor on ...".
func TestApplyRecognisesAPromptCallAndMailsItToTheMayor(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-call", "/top5 --duration 15m")

	f.apply(t)

	if got := strings.TrimSpace(f.out.String()); got != "prompt call: /top5 --duration 15m -> mw prompt run top5 --duration 15m" {
		t.Fatalf("expected the call printed as a call, got %q", got)
	}
	subjects := f.subjects(t)
	if len(subjects) != 1 || subjects[0] != "Prompt: /top5 --duration 15m" {
		t.Fatalf("expected one Prompt mail and no Governor on mail, got %v", subjects)
	}
	mail, _ := f.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if !strings.Contains(mail[0].Body, "mw prompt run top5 --duration 15m") {
		t.Fatalf("expected the mail to name the exact line to run, got %q", mail[0].Body)
	}
	if got := f.answered(t); len(got) != 0 {
		t.Fatalf("expected nothing sent back for a good call, got %v", got)
	}
	f.apply(t)
	if got := f.subjects(t); len(got) != 1 {
		t.Fatalf("expected the call mailed once however many passes see it, got %v", got)
	}
}

// A call in a bead's channel is written on that bead too.
func TestApplyCommentsAPromptCallOnTheThreadsBead(t *testing.T) {
	f := newPromptFixture(t)
	threaded, _ := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: "mw-e.2"}, Text: "/wrapup"})
	f.message(t, releaseTapGovernorKey, "tx-bead", string(threaded))

	f.apply(t)

	comments := f.tracker.Comments("mw-e.2")
	if len(comments) != 1 || !strings.HasPrefix(comments[0], "PROMPT CALL ") || !strings.Contains(comments[0], "/wrapup") {
		t.Fatalf("expected a PROMPT CALL comment on mw-e.2, got %v", comments)
	}
	if got := f.subjects(t); len(got) != 1 || got[0] != "Prompt: /wrapup" {
		t.Fatalf("expected one Prompt mail, got %v", got)
	}
}

// A prompt with a free-text option takes the words after its options: the call
// is mailed with them whole and is not refused.
func TestApplyMailsAFreeTextPromptCallWithTheWordsWhole(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-later", "/later some words")

	f.apply(t)

	subjects := f.subjects(t)
	if len(subjects) != 1 || subjects[0] != "Prompt: /later --text 'some words'" {
		t.Fatalf("expected one Prompt mail with the words whole, got %v", subjects)
	}
	mail, _ := f.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if !strings.Contains(mail[0].Body, "mw prompt run later --text 'some words'") {
		t.Fatalf("expected the mail to name the line to run, got %q", mail[0].Body)
	}
	if got := f.answered(t); len(got) != 0 {
		t.Fatalf("expected the call not refused, got %v", got)
	}
}

// A prompt the backend does not keep is answered in the thread in one line,
// and nothing is mailed.
func TestApplyAnswersAnUnknownPromptInTheThread(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-nope", "/nope")

	f.apply(t)

	if got := f.subjects(t); len(got) != 0 {
		t.Fatalf("expected no mail, got %v", got)
	}
	sent := f.answered(t)
	if len(sent) != 1 || sent[0].Text != "Unknown prompt /nope; saved prompts: /later /top5 /wrapup" || sent[0].Re != "tx-nope" {
		t.Fatalf("expected the one-line answer re the call, got %+v", sent)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-nope")); !strings.HasPrefix(note, "refused ") {
		t.Fatalf("expected the call marked applied so it is answered once, got %q", note)
	}
	f.apply(t)
	if got := f.answered(t); len(got) != 1 {
		t.Fatalf("expected one answer however many passes see it, got %v", got)
	}
}

// An option the prompt does not take is answered the same way.
func TestApplyAnswersAnUnknownPromptOption(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-bad", "/top5 --hours 2")

	f.apply(t)

	if got := f.subjects(t); len(got) != 0 {
		t.Fatalf("expected no mail, got %v", got)
	}
	sent := f.answered(t)
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "--hours is not an option") || strings.Contains(sent[0].Text, "\n") {
		t.Fatalf("expected a one-line answer naming the bad option, got %+v", sent)
	}
}

// A quoted value stays one value, and the line to run quotes it for a shell.
func TestApplyKeepsAQuotedPromptValueWhole(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-quoted", `/top5 --duration "two hours"`)

	f.apply(t)

	if got := f.subjects(t); len(got) != 1 || got[0] != `Prompt: /top5 --duration 'two hours'` {
		t.Fatalf("expected the value kept whole, got %v", got)
	}
}

// Text that does not begin with a slash, and a slash from anyone but the
// Governor, are read as before.
func TestApplyLeavesOtherMessagesAlone(t *testing.T) {
	f := newPromptFixture(t)
	threaded, _ := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: "mw-e.2"}, Text: "top5 --duration 15m"})
	f.message(t, releaseTapGovernorKey, "tx-plain", string(threaded))
	f.message(t, "someone-else-pubkey-hex", "tx-other", "/top5")

	f.apply(t)

	if got := f.subjects(t); len(got) != 1 || !strings.HasPrefix(got[0], "Governor on mw-e.2") {
		t.Fatalf("expected the plain comment mailed as it always was, got %v", got)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-other")); note != "" {
		t.Fatalf("expected another sender's slash left unapplied, got %q", note)
	}
}

// A host with no prompt backend, or one that cannot be read, leaves the call
// unapplied for the Mayor to read.
func TestApplyLeavesAPromptCallItCannotCheck(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-call", "/top5")
	f.prompts.Err = context.DeadlineExceeded

	f.apply(t)

	if got := f.subjects(t); len(got) != 0 {
		t.Fatalf("expected no mail, got %v", got)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-call")); note != "" {
		t.Fatalf("expected the call left unapplied, got %q", note)
	}
	if got := f.answered(t); len(got) != 0 {
		t.Fatalf("expected no answer, got %v", got)
	}
}

// The Mayor's own read applies a call the same way when no pass has.
func TestInboxAppliesAPromptCallThePassHasNot(t *testing.T) {
	f := newPromptFixture(t)
	f.message(t, releaseTapGovernorKey, "tx-call", "/top5 --duration 15m")

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}

	if got := strings.TrimSpace(f.out.String()); got != "prompt call: /top5 --duration 15m -> mw prompt run top5 --duration 15m" {
		t.Fatalf("expected the call printed as a call, got %q", got)
	}
	if got := f.subjects(t); len(got) != 1 || got[0] != "Prompt: /top5 --duration 15m" {
		t.Fatalf("expected one Prompt mail, got %v", got)
	}
}

package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A message in parts (postern's docs/protocol.md section 8, mw-fjpe9tx): the
// Governor's phone sends a message whose plaintext passes 7,200 bytes as
// several records naming one `split`, each its 1-based `part` of `parts`. Every
// reader of his messages must show it as one.

var partsNow = time.Date(2026, 10, 27, 18, 30, 0, 0, time.UTC)

// partsFixture is a Governor's messages in parts, in the attachment fixture's
// postern, key file and tracker, read by an inbox whose clock is partsNow.
type partsFixture struct {
	*attachmentFixture
	t *testing.T
}

func newPartsFixture(t *testing.T) *partsFixture {
	t.Helper()
	return &partsFixture{attachmentFixture: newAttachmentFixture(t), t: t}
}

// addPart indexes part k of n of the message split, sent age before partsNow.
// Part 1 alone carries re; every part carries thread (a bead when it names
// one, else a topic when that is not empty).
func (f *partsFixture) addPart(txid, split string, k, n int, text, topic, bead, re string, age time.Duration) int64 {
	f.t.Helper()
	body := map[string]any{"text": text, "split": split, "part": k, "parts": n}
	switch {
	case bead != "":
		body["thread"] = map[string]any{"bead": bead}
	case topic != "":
		body["thread"] = map[string]any{"topic": topic}
	}
	if re != "" && k == 1 {
		body["re"] = re
	}
	raw, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	ct, err := f.cipher.Encrypt(attachmentInboxPubKey, string(raw))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: attachmentInboxPubKey, Ciphertext: ct,
		Ts: partsNow.Add(-age),
	}).Seq
}

// addPlain indexes a plain message of the Governor's.
func (f *partsFixture) addPlain(txid, text string) int64 {
	f.t.Helper()
	ct, err := f.cipher.Encrypt(attachmentInboxPubKey, text)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: attachmentInboxPubKey, Ciphertext: ct,
		Ts: partsNow.Add(-time.Minute),
	}).Seq
}

func (f *partsFixture) reader(out *bytes.Buffer) application.PosternInbox {
	inbox := f.inbox(out)
	inbox.Now = func() time.Time { return partsNow }
	return inbox
}

func (f *partsFixture) cursor() int64 {
	f.t.Helper()
	saved, _ := f.tracker.Note(context.Background(), application.PosternCursorKey)
	if saved == "" {
		return 0
	}
	n, err := strconv.ParseInt(saved, 10, 64)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// AC1: three parts arriving out of order are one message: the texts joined in
// part order, the txid, re and thread of part 1.
func TestInboxPrintsAMessageInPartsAsOneMessage(t *testing.T) {
	f := newPartsFixture(t)
	f.addPart("direct:p2", "row-1", 2, 3, "two ", "week", "", "", time.Minute)
	f.addPart("direct:p3", "row-1", 3, 3, "three", "week", "", "", time.Minute)
	f.addPart("direct:p1", "row-1", 1, 3, "one ", "week", "", "direct:root", time.Minute)
	out := &bytes.Buffer{}

	messages, err := f.reader(out).Run(context.Background())
	if err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("expected one message for the three parts, got %d: %+v", len(messages), messages)
	}
	m := messages[0]
	if m.Text != "one two three" || m.Txid != "direct:p1" || m.Re != "direct:root" || m.Thread != "week" {
		t.Errorf("expected the texts in part order under part 1's txid, re and thread, got %+v", m)
	}
	if got := strings.Count(out.String(), "txid "); got != 1 {
		t.Errorf("expected one message printed, got %d headers:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "one two three") || strings.Contains(out.String(), "Missing part") {
		t.Errorf("expected the whole text and no missing part, got:\n%s", out.String())
	}
	if got := f.cursor(); got != 3 {
		t.Errorf("expected the whole message read, the cursor at 3, got %d", got)
	}
}

// AC1: --show of any part prints the whole message.
func TestInboxShowOfAnyPartPrintsTheWholeMessage(t *testing.T) {
	f := newPartsFixture(t)
	f.addPart("direct:p1", "row-2", 1, 2, "alpha ", "", "", "", time.Minute)
	f.addPart("direct:p2", "row-2", 2, 2, "omega", "", "", "", time.Minute)
	if _, err := f.reader(&bytes.Buffer{}).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	for _, txid := range []string{"direct:p1", "direct:p2"} {
		out := &bytes.Buffer{}
		if err := (application.PosternInboxShow{Dir: f.dir, GovernorKey: releaseTapGovernorKey, Out: out}).Run(context.Background(), txid); err != nil {
			t.Fatalf("--show %s: %v", txid, err)
		}
		if !strings.Contains(out.String(), "alpha omega") || !strings.Contains(out.String(), "txid direct:p1") {
			t.Errorf("--show %s: expected the whole message under part 1's txid, got:\n%s", txid, out.String())
		}
	}
}

// AC2: a message with a part still to come is printed with 'Missing part N of
// M.', is not marked read, and is shown whole when the part arrives; what was
// read past it is not shown twice.
func TestInboxHoldsAMessageWithAMissingPartUnread(t *testing.T) {
	f := newPartsFixture(t)
	f.addPart("direct:p1", "row-3", 1, 3, "one ", "", "", "", 2*time.Minute)
	f.addPart("direct:p3", "row-3", 3, 3, "three", "", "", "", time.Minute)
	f.addPlain("direct:later", "an ordinary message")
	out := &bytes.Buffer{}

	messages, err := f.reader(out).Run(context.Background())
	if err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if !strings.Contains(out.String(), "Missing part 2 of 3.") {
		t.Errorf("expected the line 'Missing part 2 of 3.', got:\n%s", out.String())
	}
	if len(messages) != 2 {
		t.Fatalf("expected the held message and the ordinary one, got %+v", messages)
	}
	if got := f.cursor(); got != 0 {
		t.Errorf("expected the message not marked read, the cursor held below its first part, got %d", got)
	}
	count, err := f.reader(&bytes.Buffer{}).UnreadCount(context.Background())
	if err != nil || count != 1 {
		t.Errorf("expected one unread (the held message; the ordinary one was shown), got %d, %v", count, err)
	}

	// The missing part arrives: the message is whole, shown once more, then read.
	f.addPart("direct:p2", "row-3", 2, 3, "two ", "", "", "", 0)
	out.Reset()
	if _, err := f.reader(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox again: %v", err)
	}
	if !strings.Contains(out.String(), "one two three") || strings.Contains(out.String(), "Missing part") {
		t.Errorf("expected the whole message, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "an ordinary message") {
		t.Errorf("expected the ordinary message, already shown, not shown twice, got:\n%s", out.String())
	}
	if got := f.cursor(); got != 4 {
		t.Errorf("expected the cursor past all four records, got %d", got)
	}
}

// AC2: ten minutes after its first part, a message still lacking one is read.
func TestInboxReadsAMessageWhoseMissingPartDidNotComeInTenMinutes(t *testing.T) {
	f := newPartsFixture(t)
	f.addPart("direct:p1", "row-4", 1, 2, "only the first", "", "", "", 11*time.Minute)
	out := &bytes.Buffer{}

	if _, err := f.reader(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if !strings.Contains(out.String(), "only the first") || !strings.Contains(out.String(), "Missing part 2 of 2.") {
		t.Errorf("expected what came and the missing line, got:\n%s", out.String())
	}
	if got := f.cursor(); got != 1 {
		t.Errorf("expected the message read after ten minutes, the cursor at 1, got %d", got)
	}
}

// AC2: a message held unread is read, with what came, once its ten minutes are
// over, though an earlier read showed it.
func TestInboxReadsAHeldMessageWhenItsTenMinutesAreOver(t *testing.T) {
	f := newPartsFixture(t)
	f.addPart("direct:p1", "row-9", 1, 2, "the half", "", "", "", 5*time.Minute)
	if _, err := f.reader(&bytes.Buffer{}).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if got := f.cursor(); got != 0 {
		t.Fatalf("expected the message held unread, got the cursor at %d", got)
	}

	later := f.inbox(&bytes.Buffer{})
	out := &bytes.Buffer{}
	later.Out = out
	later.Now = func() time.Time { return partsNow.Add(6 * time.Minute) }
	if _, err := later.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox later: %v", err)
	}
	if !strings.Contains(out.String(), "the half") || !strings.Contains(out.String(), "Missing part 2 of 2.") {
		t.Errorf("expected what came with the missing line, got:\n%s", out.String())
	}
	if got := f.cursor(); got != 1 {
		t.Errorf("expected the message read after its ten minutes, got the cursor at %d", got)
	}
}

// AC3: a bead-channel message in parts is applied as a comment once whole, and
// as the whole text, never as one part.
func TestApplyCommentsTheJoinedMessageOnTheBead(t *testing.T) {
	f := newPartsFixture(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-parts.1"})
	f.addPart("direct:p1", "row-5", 1, 2, "The first half. ", "", "mw-parts.1", "", time.Minute)

	if _, err := f.reader(&bytes.Buffer{}).Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	comments, _ := f.tracker.StoryComments(context.Background(), "mw-parts.1")
	if len(comments) != 0 {
		t.Fatalf("expected nothing applied while a part is missing, got %+v", comments)
	}

	f.addPart("direct:p2", "row-5", 2, 2, "The second half.", "", "mw-parts.1", "", 0)
	if _, err := f.reader(&bytes.Buffer{}).Apply(context.Background()); err != nil {
		t.Fatalf("applying again: %v", err)
	}
	comments, _ = f.tracker.StoryComments(context.Background(), "mw-parts.1")
	if len(comments) != 1 || !strings.Contains(comments[0].Text, "The first half. The second half.") {
		t.Fatalf("expected one comment holding both halves, got %+v", comments)
	}
}

// AC3: the same, when the parts never all come: after ten minutes the comment
// is the text that came, with the missing line.
func TestApplyCommentsWhatCameAfterTenMinutes(t *testing.T) {
	f := newPartsFixture(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-parts.2"})
	f.addPart("direct:p1", "row-6", 1, 2, "Half of it.", "", "mw-parts.2", "", 11*time.Minute)

	if _, err := f.reader(&bytes.Buffer{}).Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	comments, _ := f.tracker.StoryComments(context.Background(), "mw-parts.2")
	if len(comments) != 1 || !strings.Contains(comments[0].Text, "Half of it.") || !strings.Contains(comments[0].Text, "Missing part 2 of 2.") {
		t.Fatalf("expected the comment to say what came and what is missing, got %+v", comments)
	}
}

// addMessagePart indexes part k of n of a message in parts to the Mayor, as the
// talk wait fixtures index a turn's.
func (f *talkPosternFixture) addMessagePart(txid, split string, k, n int, text string) int64 {
	f.t.Helper()
	plain, _ := json.Marshal(map[string]any{"text": text, "split": split, "part": k, "parts": n})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(plain))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
		Ts: time.Date(2026, 10, 27, 18, 1, k, 0, time.UTC),
	}).Seq
}

// AC3: mw talk wait holds a message in parts until it is whole, and reports it
// as one message.
func TestTalkWaitReportsAMessageInPartsAsOne(t *testing.T) {
	f := newTalkPosternFixture(t)
	report, printed, _ := f.runParts(time.Minute,
		func() int64 { return f.addMessagePart("direct:m2", "row-7", 2, 3, "two ") },
		func() int64 { return f.addMessagePart("direct:m1", "row-7", 1, 3, "one ") },
		func() int64 { return f.addMessagePart("direct:m3", "row-7", 3, 3, "three") },
	)

	if len(report.Postern) != 1 || report.Postern[0].Text != "one two three" || report.Postern[0].Txid != "direct:m1" {
		t.Fatalf("expected one message, the texts joined, under part 1's txid, got %+v", report.Postern)
	}
	if !strings.Contains(printed, "1 unread") || !strings.Contains(printed, "one two three") || strings.Contains(printed, "Missing part") {
		t.Errorf("expected one unread message printed whole, got:\n%s", printed)
	}
}

// AC3: a part that does not come within the parts wait is named.
func TestTalkWaitSaysWhichPartOfAMessageIsMissing(t *testing.T) {
	f := newTalkPosternFixture(t)
	report, printed, _ := f.runParts(50*time.Millisecond,
		func() int64 { return f.addMessagePart("direct:m1", "row-8", 1, 3, "one ") },
		func() int64 { return f.addMessagePart("direct:m3", "row-8", 3, 3, "three") },
	)

	if len(report.Postern) != 1 || report.Postern[0].Text != "one three\n\nMissing part 2 of 3." {
		t.Fatalf("expected one message, what came then the missing line, got %+v", report.Postern)
	}
	if !strings.Contains(printed, "Missing part 2 of 3.") {
		t.Errorf("expected the missing part named, got:\n%s", printed)
	}
}

// Sweep: mw postern send of a text over the record's plaintext budget refuses
// in plain words, sending nothing; a text under it goes.
func TestPosternSendRefusesATextOverTheRecordBudget(t *testing.T) {
	backend := apptest.NewFakePostern()
	send := application.PosternSend{
		Postern: backend, Cipher: apptest.NewFakeCipher(), Keys: stubPosternKeys{pubKey: attachmentInboxPubKey},
		GovernorKey: releaseTapGovernorKey, Channel: application.PosternChannelDirect,
		Out: &bytes.Buffer{},
	}

	_, err := send.Run(context.Background(), application.PosternSendRequest{Text: strings.Repeat("a", application.PosternPlaintextMax+1)})
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected a plain refusal for a text over %d bytes, got %v", application.PosternPlaintextMax, err)
	}
	if len(backend.Delivered()) != 0 {
		t.Errorf("expected nothing sent, got %d records", len(backend.Delivered()))
	}
	if _, err := send.Run(context.Background(), application.PosternSendRequest{Text: strings.Repeat("a", application.PosternPlaintextMax)}); err != nil {
		t.Errorf("expected a text at the budget to go, got %v", err)
	}
}

package application_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

const sendMayorKey = "mayor-pubkey-hex"

// sendFixture is a PosternSend over fakes: the Mayor's key, a backend, a
// cipher whose envelope names the Mayor, and a tracker holding one bead.
type sendFixture struct {
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	tracker *apptest.FakeTracker
	out     bytes.Buffer
}

func newSendFixture() *sendFixture {
	cipher := apptest.NewFakeCipher()
	cipher.From = sendMayorKey
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-a", domain.Path{})
	tracker.AddStory("mw-a", domain.Story{ID: "mw-a.1", Title: "A story"})
	return &sendFixture{backend: apptest.NewFakePostern(), cipher: cipher, tracker: tracker}
}

func (f *sendFixture) send(channel string) application.PosternSend {
	return application.PosternSend{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: sendMayorKey},
		Tracker: f.tracker, Notes: f.tracker,
		GovernorKey: releaseTapGovernorKey, FloatSats: 100000, Channel: channel,
		Now: func() time.Time { return time.Unix(1758700000, 0) }, Out: &f.out,
	}
}

// delivered is the n-th payload delivered directly, and its plaintext.
func (f *sendFixture) delivered(t *testing.T, n int) (application.PosternPayload, string) {
	t.Helper()
	sent := f.backend.Delivered()
	if len(sent) <= n {
		t.Fatalf("expected at least %d direct deliveries, got %d", n+1, len(sent))
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(sent[n], &payload); err != nil {
		t.Fatalf("the delivered payload is not JSON: %v", err)
	}
	text, _, err := f.cipher.Decrypt("governor-private-key", payload.Ct)
	if err != nil {
		t.Fatalf("decrypting the delivered plaintext: %v", err)
	}
	return payload, text
}

// A message goes by the direct channel unless the chain is asked for: the
// record payload is delivered to the backend, no coin is looked at, spent
// or broadcast, and the direct id is printed.
func TestPosternSendDeliversDirectlyByDefault(t *testing.T) {
	f := newSendFixture()
	f.backend.SetBalance("", 999999999)

	txid, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "Ready for review."})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	if !strings.HasPrefix(txid, "direct:") || strings.TrimSpace(f.out.String()) != txid {
		t.Fatalf("expected the direct id printed, got %q and %q", txid, f.out.String())
	}
	if len(f.backend.Broadcasts()) != 0 {
		t.Fatalf("expected nothing broadcast on the direct channel, got %d", len(f.backend.Broadcasts()))
	}
	payload, text := f.delivered(t, 0)
	if payload.V != 1 || payload.Kind != "msg" || payload.Class != "message" || payload.To != releaseTapGovernorKey ||
		payload.From != sendMayorKey || payload.Ts != 1758700000 || text != "Ready for review." {
		t.Fatalf("expected section 1's payload of the plain text, got %+v with %q", payload, text)
	}
}

// The chain channel is still the funded transaction, float cap and all.
func TestPosternSendOnTheChainChannelKeepsTheFloatCap(t *testing.T) {
	f := newSendFixture()
	f.backend.SetBalance("", 150000)

	_, err := f.send(application.PosternChannelChain).Run(context.Background(), application.PosternSendRequest{Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "50000") {
		t.Fatalf("expected the chain channel refused over the float cap, naming the excess, got %v", err)
	}
	if len(f.backend.Delivered()) != 0 {
		t.Fatal("expected nothing delivered directly on the chain channel")
	}
	if _, err := f.send("pigeon").Run(context.Background(), application.PosternSendRequest{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "pigeon") {
		t.Fatalf("expected an unknown channel refused, got %v", err)
	}
}

// A question still records itself on its bead, on the direct channel as on
// the chain, and its note now says what it asked.
func TestPosternSendAsksAQuestionOnTheDirectChannel(t *testing.T) {
	f := newSendFixture()

	txid, err := f.send("").Run(context.Background(), application.PosternSendRequest{
		Class: "decision-needed", Text: "Ship it?", Bead: "mw-a.1", Recommend: "A", Options: []string{"A", "B"},
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	comments := f.tracker.Comments("mw-a.1")
	if len(comments) != 1 || !strings.HasPrefix(comments[0], "QUESTION 2025-09-24T07:46:40Z asked by postern, txid "+txid+": Ship it?") {
		t.Fatalf("expected the QUESTION comment with the direct id, got %v", comments)
	}
	note, _ := f.tracker.Note(context.Background(), application.PosternQuestionKey("mw-a.1"))
	var saved struct{ Txid, Q, Rec, Asked string }
	if err := json.Unmarshal([]byte(note), &saved); err != nil || saved.Txid != txid || saved.Q != "Ship it?" || saved.Rec != "A" || saved.Asked == "" {
		t.Fatalf("expected the note to say what was asked, got %q: %v", note, err)
	}
}

// A message in a bead's thread is written to the bead too, as the Mayor's
// side of the exchange.
func TestPosternSendCommentsTheBeadAMessageIsThreadedOn(t *testing.T) {
	f := newSendFixture()

	txid, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "Landed; have a look.", Thread: "mw-a.1"})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	if got := f.tracker.Comments("mw-a.1"); len(got) != 1 || got[0] != "MAYOR via postern, txid "+txid+": Landed; have a look." {
		t.Fatalf("expected the MAYOR comment, got %v", got)
	}
	_, text := f.delivered(t, 0)
	if text != `{"thread":{"bead":"mw-a.1"},"text":"Landed; have a look."}` {
		t.Fatalf("expected the threaded plaintext, got %s", text)
	}
}

// A transcript sent back names the voice note it annotates and its role, and
// is not the Mayor's own word on the bead.
func TestPosternSendATranscriptCarriesReAndRoleAndIsNotCommented(t *testing.T) {
	f := newSendFixture()

	if _, err := f.send("").Run(context.Background(), application.PosternSendRequest{
		Text: "what was heard", Thread: "mw-a.1", Re: "direct:voice", Role: application.PosternRoleTranscript,
	}); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if got := f.tracker.Comments("mw-a.1"); len(got) != 0 {
		t.Fatalf("expected no comment for a transcript, got %v", got)
	}
	_, text := f.delivered(t, 0)
	if text != `{"thread":{"bead":"mw-a.1"},"text":"what was heard","re":"direct:voice","role":"transcript"}` {
		t.Fatalf("expected section 14's transcript body, got %s", text)
	}
}

// --attach encrypts the file to the Governor, uploads the ciphertext, and
// announces it in the message: its hash, size and the mime its extension
// names.
func TestPosternSendAttachesAFile(t *testing.T) {
	f := newSendFixture()
	dir := t.TempDir()
	pdf := filepath.Join(dir, "report.pdf")
	content := []byte("%PDF-1.7 not really\x00\xff")
	mustDo(t, os.WriteFile(pdf, content, 0o600))

	if _, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "the report", Attachments: []string{pdf}}); err != nil {
		t.Fatalf("sending: %v", err)
	}
	uploaded := f.backend.Uploaded()
	if len(uploaded) != 1 {
		t.Fatalf("expected one upload, got %d", len(uploaded))
	}
	plain, _, err := f.cipher.Decrypt("any", base64.StdEncoding.EncodeToString(uploaded[0]))
	if err != nil || plain != string(content) {
		t.Fatalf("expected the upload to be the file's bytes encrypted, got %q: %v", plain, err)
	}
	sum := sha256.Sum256(uploaded[0])
	_, text := f.delivered(t, 0)
	var body application.PosternThreadedMessage
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("the plaintext is not JSON: %v", err)
	}
	if body.Text != "the report" || body.Attachment == nil || body.Attachment.Hash != hex.EncodeToString(sum[:]) ||
		body.Attachment.Size != int64(len(uploaded[0])) || body.Attachment.Mime != "application/pdf" {
		t.Fatalf("expected the attachment announced, got %+v", body)
	}
	if strings.Contains(text, `"thread"`) {
		t.Fatalf("expected no thread field for the general thread, got %s", text)
	}
}

// Several files are several messages, the caption on the last.
func TestPosternSendSeveralFilesAreSeveralMessagesTheCaptionOnTheLast(t *testing.T) {
	f := newSendFixture()
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.ogg")
	mustDo(t, os.WriteFile(a, []byte("png"), 0o600))
	mustDo(t, os.WriteFile(b, []byte("ogg"), 0o600))

	last, err := f.send("").Run(context.Background(), application.PosternSendRequest{Text: "both", Thread: "mw-a.1", Attachments: []string{a, b}})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}
	_, first := f.delivered(t, 0)
	_, second := f.delivered(t, 1)
	var one, two application.PosternThreadedMessage
	mustDo(t, json.Unmarshal([]byte(first), &one))
	mustDo(t, json.Unmarshal([]byte(second), &two))
	if one.Text != "" || one.Attachment.Mime != "image/png" || two.Text != "both" || two.Attachment.Mime != "audio/ogg" ||
		one.Thread.Bead != "mw-a.1" || two.Thread.Bead != "mw-a.1" {
		t.Fatalf("expected the png uncaptioned then the ogg captioned, both in the thread, got %s and %s", first, second)
	}
	if lines := strings.Split(strings.TrimSpace(f.out.String()), "\n"); len(lines) != 2 || lines[1] != last {
		t.Fatalf("expected both ids printed, the last returned, got %q", f.out.String())
	}
	if got := f.tracker.Comments("mw-a.1"); len(got) != 1 || !strings.Contains(got[0], last) || !strings.Contains(got[0], "a.png") || !strings.Contains(got[0], "b.ogg") {
		t.Fatalf("expected one MAYOR comment naming both files, got %v", got)
	}
}

// Nothing is sent when a file cannot be: too large, of a type §14 does not
// list, or with a question, whose shape carries no attachment.
func TestPosternSendRefusesAnAttachmentItCannotSendBeforeSendingAnything(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.png")
	mustDo(t, os.WriteFile(big, bytes.Repeat([]byte{1}, 8<<20+1), 0o600))
	exe := filepath.Join(dir, "tool.exe")
	mustDo(t, os.WriteFile(exe, []byte("MZ"), 0o600))
	small := filepath.Join(dir, "ok.png")
	mustDo(t, os.WriteFile(small, []byte("png"), 0o600))

	for name, req := range map[string]application.PosternSendRequest{
		"8 MiB":          {Text: "x", Attachments: []string{small, big}},
		".exe":           {Text: "x", Attachments: []string{exe}},
		"decision":       {Class: "decision-needed", Text: "x", Bead: "mw-a.1", Options: []string{"A"}, Attachments: []string{small}},
		"no such file":   {Text: "x", Attachments: []string{filepath.Join(dir, "missing.png")}},
		"chain, several": {Text: "x", Attachments: []string{small, small}},
	} {
		f := newSendFixture()
		channel := ""
		if name == "chain, several" {
			channel = application.PosternChannelChain
		}
		if _, err := f.send(channel).Run(context.Background(), req); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
		if len(f.backend.Uploaded()) != 0 || len(f.backend.Delivered()) != 0 || len(f.backend.Broadcasts()) != 0 {
			t.Errorf("%s: expected nothing uploaded or sent", name)
		}
	}
}

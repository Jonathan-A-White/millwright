package application_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// voiceFixture is the apply fixture with a transcriber, somewhere to write
// attachments, and a sender to hand transcripts back through.
type voiceFixture struct {
	*applyFixture
	transcriber *apptest.FakeTranscriber
	dir         string
	audio       []byte
}

func newVoiceFixture(t *testing.T) *voiceFixture {
	t.Helper()
	return &voiceFixture{
		applyFixture: newApplyFixture(t),
		transcriber:  &apptest.FakeTranscriber{Transcript: "  ship the storage engine as planned  "},
		dir:          filepath.Join(t.TempDir(), "postern", "inbox"),
		audio:        []byte("OggS pretend opus frames \x00\x01"),
	}
}

// voiceNote adds a verified message from `from` in thread carrying the audio
// as an attachment of mime, its caption text.
func (f *voiceFixture) voiceNote(t *testing.T, from, txid string, thread application.PosternThread, mime, caption string) {
	t.Helper()
	f.cipher.From = from
	sealed, err := f.cipher.EncryptBytes(applyInboxKey, f.audio)
	mustDo(t, err)
	raw, err := base64.StdEncoding.DecodeString(sealed)
	mustDo(t, err)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	f.backend.SetBlob(hash, raw)
	body, err := json.Marshal(application.PosternThreadedMessage{
		Thread: thread, Text: caption, Attachment: &application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: mime},
	})
	mustDo(t, err)
	f.message(t, from, txid, string(body))
}

func (f *voiceFixture) inbox() application.PosternInbox {
	inbox := f.applyFixture.inbox()
	inbox.AttachmentDir = f.dir
	inbox.Transcriber = f.transcriber
	inbox.Sender = &application.PosternSend{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: applyInboxKey},
		GovernorKey: releaseTapGovernorKey,
	}
	return inbox
}

// sentBack is the n-th message delivered back to the Governor, decrypted.
func (f *voiceFixture) sentBack(t *testing.T, n int) application.PosternThreadedMessage {
	t.Helper()
	delivered := f.backend.Delivered()
	if len(delivered) <= n {
		t.Fatalf("expected at least %d message(s) sent back, got %d", n+1, len(delivered))
	}
	var payload application.PosternPayload
	mustDo(t, json.Unmarshal(delivered[n], &payload))
	text, _, err := f.cipher.Decrypt("governor", payload.Ct)
	mustDo(t, err)
	var body application.PosternThreadedMessage
	mustDo(t, json.Unmarshal([]byte(text), &body))
	return body
}

// A Governor's voice note in a bead's thread is heard on this host: the audio
// is downloaded and decrypted, the transcriber run on it, what was heard
// written on the bead as his words, sent back to him in the same thread as a
// transcript of the note, and mailed to the Mayor — once.
func TestApplyTranscribesAVoiceNoteInABeadsThread(t *testing.T) {
	f := newVoiceFixture(t)
	f.voiceNote(t, releaseTapGovernorKey, "direct:v1", application.PosternThread{Bead: "mw-e.3"}, "audio/webm;codecs=opus", "")

	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying again: %v", err)
	}

	paths, contents := f.transcriber.Heard()
	if len(paths) != 1 {
		t.Fatalf("expected the note heard once, got %v", paths)
	}
	if want := filepath.Join(f.dir, "direct-v1.webm"); paths[0] != want || string(contents[0]) != string(f.audio) {
		t.Fatalf("expected the decrypted audio at %s, got %s holding %q", want, paths[0], contents[0])
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || got[0] != "GOVERNOR (voice) via postern, txid direct:v1: ship the storage engine as planned" {
		t.Fatalf("expected the transcript on the bead as the Governor's words, got %v", got)
	}
	back := f.sentBack(t, 0)
	if back.Thread.Bead != "mw-e.3" || back.Text != "ship the storage engine as planned" || back.Re != "direct:v1" || back.Role != "transcript" {
		t.Fatalf("expected the transcript sent back in the bead's thread, re the note, got %+v", back)
	}
	if n := len(f.backend.Delivered()); n != 1 {
		t.Fatalf("expected one transcript sent back, got %d", n)
	}
	if subjects := f.subjects(t); len(subjects) != 1 || !strings.HasPrefix(subjects[0], "Voice: mw-e.3: ship the storage") {
		t.Fatalf("expected one Voice mail to the Mayor, got %v", subjects)
	}

	f.out.Reset()
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := strings.TrimSpace(f.out.String()); !strings.HasPrefix(got, "already applied earlier: applied voice mw-e.3 txid direct:v1") {
		t.Fatalf("expected the Mayor's read to show the note as applied, got %q", got)
	}
}

// A voice note on a topic is not written to any bead; what was heard is sent
// back on the topic, and the Mayor's read prints it in the note's one line.
func TestApplyTranscribesAVoiceNoteOnATopicAndTheMayorReadsIt(t *testing.T) {
	f := newVoiceFixture(t)
	f.voiceNote(t, releaseTapGovernorKey, "direct:v2", application.PosternThread{Topic: "roadmap"}, "audio/ogg", "")

	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if back := f.sentBack(t, 0); back.Thread.Topic != "roadmap" || back.Re != "direct:v2" || back.Role != "transcript" {
		t.Fatalf("expected the transcript sent back on the topic, got %+v", back)
	}
	f.out.Reset()
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := strings.TrimSpace(f.out.String()); got != "already applied earlier: applied voice roadmap txid direct:v2: ship the storage engine as planned" {
		t.Fatalf("expected the transcript in the applied line, got %q", got)
	}
}

// A transcriber that fails leaves the note unheard: the Mayor is told, and it
// is never tried again.
func TestApplyTellsTheMayorWhenAVoiceNoteCannotBeHeard(t *testing.T) {
	f := newVoiceFixture(t)
	f.transcriber.Err = fmt.Errorf("whisper-cli: no model")
	f.voiceNote(t, releaseTapGovernorKey, "direct:v3", application.PosternThread{Bead: "mw-e.3"}, "audio/mpeg", "")

	for i := 0; i < 2; i++ {
		if _, err := f.inbox().Apply(context.Background()); err != nil {
			t.Fatalf("applying: %v", err)
		}
	}
	if paths, _ := f.transcriber.Heard(); len(paths) != 1 {
		t.Fatalf("expected one try, got %d", len(paths))
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 0 {
		t.Fatalf("expected nothing written on the bead, got %v", got)
	}
	if subjects := f.subjects(t); len(subjects) != 1 || subjects[0] != "Voice note not heard: mw-e.3" {
		t.Fatalf("expected the Mayor told once, got %v", subjects)
	}
}

// With no transcriber configured, a voice note in a bead's thread is the
// Governor's comment like any other, naming the audio's path.
func TestAVoiceNoteWithNoTranscriberIsAnOrdinaryComment(t *testing.T) {
	f := newVoiceFixture(t)
	f.voiceNote(t, releaseTapGovernorKey, "direct:v4", application.PosternThread{Bead: "mw-e.3"}, "audio/webm", "listen")
	inbox := f.inbox()
	inbox.Transcriber = nil

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	got := f.tracker.Comments("mw-e.3")
	if len(got) != 1 || !strings.HasSuffix(got[0], fmt.Sprintf("listen [audio: %s]", filepath.Join(f.dir, "direct-v4.webm"))) {
		t.Fatalf("expected an ordinary comment naming the audio, got %v", got)
	}
}

// Only the Governor's voice notes are heard.
func TestApplyHearsOnlyTheGovernorsVoiceNotes(t *testing.T) {
	f := newVoiceFixture(t)
	f.voiceNote(t, "someone-else-pubkey-hex", "direct:v5", application.PosternThread{Bead: "mw-e.3"}, "audio/webm", "")

	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if paths, _ := f.transcriber.Heard(); len(paths) != 0 {
		t.Fatalf("expected nothing heard, got %v", paths)
	}
}

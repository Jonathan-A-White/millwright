package application_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// addAttachment indexes a message from the Governor to the Mayor carrying one
// file of the mime, its words text, and returns its seq. The file is stored in
// the fake blob store, sealed as the phone seals it.
func (f *talkPosternFixture) addAttachment(txid, mime, text string, audio []byte) int64 {
	f.t.Helper()
	sealed, err := f.cipher.EncryptBytes(talkWaitMayorKey, audio)
	if err != nil {
		f.t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		f.t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	f.postern.SetBlob(hash, raw)
	body, _ := json.Marshal(application.PosternThreadedMessage{
		Text: text, Attachment: &application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: mime},
	})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(body))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq
}

// runHearing is run for a wait that can hear a voice note with transcriber,
// writing the audio under a temp dir; nil transcriber is a wait that cannot.
func (f *talkPosternFixture) runHearing(head int64, transcriber application.PosternTranscriber) string {
	f.t.Helper()
	var out strings.Builder
	_, err := application.TalkWait{
		Stream:        talkWaitStream{seq: head},
		Postern:       f.postern,
		Cipher:        apptest.NewFakeCipher(),
		Keys:          stubPosternKeys{pubKey: talkWaitMayorKey},
		Memory:        f.tracker,
		GovernorKey:   talkWaitGovernorKey,
		Transcriber:   transcriber,
		AttachmentDir: f.t.TempDir(),
		Limit:         200 * time.Millisecond,
		MinBackoff:    5 * time.Millisecond,
		MaxBackoff:    5 * time.Millisecond,
		Out:           &out,
	}.Run(context.Background())
	if err != nil {
		f.t.Fatalf("the wait: %v", err)
	}
	return out.String()
}

// A voice note with no typed words is a voice note, never "(no text)", and says
// where its words will come from.
func TestTalkWaitCallsAVoiceNoteAVoiceNote(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addAttachment("direct:v1", "audio/webm;codecs=opus", "", []byte("audio"))

	_, printed := f.run(seq)

	if !strings.Contains(printed, "voice note (words to follow by mail Voice: ...; read with mw postern inbox)") {
		t.Errorf("expected the voice note named, its words to follow, got:\n%s", printed)
	}
	if strings.Contains(printed, "(no text)") {
		t.Errorf("a voice note is never (no text), got:\n%s", printed)
	}
}

// Typed words with the note follow the voice-note mark.
func TestTalkWaitPrintsAVoiceNotesTypedWords(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addAttachment("direct:v2", "audio/webm", "Important", []byte("audio"))

	_, printed := f.run(seq)

	if !strings.Contains(printed, "voice note (words to follow") || !strings.Contains(printed, "with the words: Important") {
		t.Errorf("expected the voice-note mark and the typed words, got:\n%s", printed)
	}
}

// A transcript the inbox already heard is carried in quotes.
func TestTalkWaitCarriesATranscriptTheInboxAlreadyHeard(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addAttachment("direct:v3", "audio/ogg", "Important", []byte("audio"))
	line := "applied voice general txid direct:v3: Same with type clipboard."
	if err := f.tracker.SetNote(context.Background(), application.PosternAppliedKey("direct:v3"), line); err != nil {
		t.Fatal(err)
	}

	_, printed := f.run(seq)

	if !strings.Contains(printed, `voice note: "Same with type clipboard."`) || !strings.Contains(printed, "with the words: Important") {
		t.Errorf("expected the transcript in quotes and the typed words, got:\n%s", printed)
	}
	if strings.Contains(printed, "to follow") {
		t.Errorf("expected no 'to follow' once the words are known, got:\n%s", printed)
	}
}

// With a transcriber the wait hears the note itself, so its words are in the notice.
func TestTalkWaitHearsAVoiceNoteBeforePrintingIt(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addAttachment("direct:v4", "audio/webm", "", []byte("the audio"))
	hearer := &apptest.FakeTranscriber{Transcript: "Oh, stop typing as important."}

	printed := f.runHearing(seq, hearer)

	if !strings.Contains(printed, `voice note: "Oh, stop typing as important."`) {
		t.Errorf("expected the words heard in the notice, got:\n%s", printed)
	}
	if _, contents := hearer.Heard(); len(contents) != 1 || string(contents[0]) != "the audio" {
		t.Errorf("expected the decrypted audio heard once, got %q", contents)
	}
}

// A note that cannot be heard still gets the 'to follow' line.
func TestTalkWaitFallsBackWhenAVoiceNoteCannotBeHeard(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addAttachment("direct:v5", "audio/webm", "", []byte("the audio"))

	printed := f.runHearing(seq, &apptest.FakeTranscriber{Err: context.DeadlineExceeded})

	if !strings.Contains(printed, "voice note (words to follow") || strings.Contains(printed, "(no text)") {
		t.Errorf("expected the 'to follow' line, got:\n%s", printed)
	}
}

// A file that is not audio keeps "(no text)".
func TestTalkWaitStillSaysNoTextForAFileThatIsNotAudio(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addAttachment("direct:p1", "image/png", "", []byte("png"))

	_, printed := f.run(seq)

	if !strings.Contains(printed, "(no text)") || strings.Contains(printed, "voice note") {
		t.Errorf("expected (no text) and no voice note, got:\n%s", printed)
	}
}

package application_test

import (
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

// A turn's plaintext decodes with its picture into the one attachment type a
// message's file decodes into, and without it to none.
func TestTalkTurnDecodesWithAndWithoutAnAttachment(t *testing.T) {
	var with application.TalkTurn
	raw := `{"talk":{"id":"talk-12","turn":1},"role":"turn","text":"Look at this",` +
		`"attachment":{"hash":"ab12","size":42,"mime":"image/jpeg","name":"Screenshot.jpg"}}`
	if err := json.Unmarshal([]byte(raw), &with); err != nil {
		t.Fatal(err)
	}
	var shared *application.PosternAttachment = with.Attachment
	if shared == nil || *shared != (application.PosternAttachment{Hash: "ab12", Size: 42, Mime: "image/jpeg", Name: "Screenshot.jpg"}) {
		t.Errorf("expected the attachment decoded, got %+v", shared)
	}

	var without application.TalkTurn
	if err := json.Unmarshal([]byte(`{"talk":{"id":"talk-12","turn":2},"role":"turn","text":"Plain"}`), &without); err != nil {
		t.Fatal(err)
	}
	if without.Attachment != nil {
		t.Errorf("expected no attachment, got %+v", without.Attachment)
	}
	if encoded, _ := json.Marshal(without); strings.Contains(string(encoded), "attachment") {
		t.Errorf("expected a turn with no picture to encode no attachment field, got %s", encoded)
	}
}

// addPictureTurn indexes a talk turn from the Governor to the Mayor carrying
// picture as a sealed image/jpeg named name, and returns its seq and the hash
// the blob is stored under.
func (f *talkPosternFixture) addPictureTurn(txid, text, name string, picture []byte) (int64, string) {
	f.t.Helper()
	sealed, err := f.cipher.EncryptBytes(talkWaitMayorKey, picture)
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
	plain, _ := json.Marshal(map[string]any{
		"talk": map[string]any{"id": "talk-12", "turn": 1}, "text": text, "role": application.TalkRoleTurn,
		"attachment": map[string]any{"hash": hash, "size": len(raw), "mime": "image/jpeg", "name": name},
	})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(plain))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "talk", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq, hash
}

// runInto is run with the turn's pictures written under dir.
func (f *talkPosternFixture) runInto(head int64, dir string) string {
	f.t.Helper()
	var out strings.Builder
	_, err := application.TalkWait{
		Stream:        talkWaitStream{seq: head},
		Postern:       f.postern,
		Cipher:        apptest.NewFakeCipher(),
		Keys:          stubPosternKeys{pubKey: talkWaitMayorKey},
		Memory:        f.tracker,
		GovernorKey:   talkWaitGovernorKey,
		AttachmentDir: dir,
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

// A turn's picture is written to the inbox dir under the record's txid and
// printed under the turn's text; a run that finds it there fetches nothing.
func TestTalkWaitWritesATurnsPictureAndPrintsItsPath(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	seq, hash := f.addPictureTurn("direct:pic1", "Look at this", "Screenshot.jpg", []byte("jpeg bytes"))

	printed := f.runInto(seq, dir)

	path := filepath.Join(dir, "direct-pic1.jpg")
	line := "text: Look at this\nsaved: " + filepath.Join(dir, "direct-pic1.txt") + "\nimage: " + path + " (Screenshot.jpg)\n"
	if !strings.Contains(printed, line) {
		t.Errorf("expected %q under the text, got:\n%s", line, printed)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "jpeg bytes" {
		t.Errorf("expected the decrypted picture at %s, got %q (%v)", path, got, err)
	}

	// The blob gone from the backend, a second run can only print the line
	// from the file it finds.
	if err := f.postern.DeleteBlob(context.Background(), hash); err != nil {
		t.Fatal(err)
	}
	if err := f.tracker.SetNote(context.Background(), application.TalkWaitCursorKey, "0"); err != nil {
		t.Fatal(err)
	}
	if again := f.runInto(seq, dir); !strings.Contains(again, line) {
		t.Errorf("expected the same line on a second run, got:\n%s", again)
	}
}

// A picture that cannot be fetched is said so, and the turn is printed whole.
func TestTalkWaitSaysAPictureCouldNotBeFetched(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq, hash := f.addPictureTurn("direct:pic2", "Look at this", "Screenshot.jpg", []byte("jpeg bytes"))
	if err := f.postern.DeleteBlob(context.Background(), hash); err != nil {
		t.Fatal(err)
	}

	printed := f.runInto(seq, t.TempDir())

	for _, want := range []string{"talk talk-12 turn 1", "cut no", "text: Look at this\nsaved: ", "index-to-print "} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected %q, got:\n%s", want, printed)
		}
	}
}

// A turn with no picture prints no image line.
func TestTalkWaitPrintsNoImageLineForATurnWithoutAPicture(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addTurn("Plain question")

	printed := f.runInto(seq, t.TempDir())

	if !strings.Contains(printed, "text: Plain question") || strings.Contains(printed, "image:") {
		t.Errorf("expected the turn and no image line, got:\n%s", printed)
	}
}

// The turn's full text is written to <dir>/<txid>.txt before it is printed,
// and 'saved: <path>' is printed under the text line (mw-gq6.515).
func TestTalkWaitSavesATurnsWholeTextAndPrintsThePath(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	long := strings.Repeat("a long, long word of the Governor's é ", 60) + "\nand the end of it"
	seq := f.addTurn(long)

	printed := f.runInto(seq, dir)

	path := filepath.Join(dir, "direct-turn.txt")
	if got, err := os.ReadFile(path); err != nil || string(got) != long {
		t.Errorf("expected exactly the turn's text at %s, got %q (%v)", path, got, err)
	}
	if want := "text: " + long + "\nsaved: " + path + "\n"; !strings.Contains(printed, want) {
		t.Errorf("expected the saved line under the text, got:\n%s", printed)
	}
}

// With no dir to save in the turn is printed as ever, with no saved line.
func TestTalkWaitPrintsNoSavedLineWithoutADir(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addTurn("Plain question")

	_, printed := f.run(seq)

	if !strings.Contains(printed, "text: Plain question") || strings.Contains(printed, "saved:") {
		t.Errorf("expected the turn and no saved line, got:\n%s", printed)
	}
}

// addTurnOf indexes a talk turn of the talk id, number turn, from the Governor.
func (f *talkPosternFixture) addTurnOf(txid, id string, turn int, text string) int64 {
	f.t.Helper()
	plain, _ := json.Marshal(map[string]any{
		"talk": map[string]any{"id": id, "turn": turn}, "text": text, "role": application.TalkRoleTurn,
	})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(plain))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "talk", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
		Ts: time.Date(2026, 10, 16, 13, 9, int(turn), 0, time.UTC),
	}).Seq
}

func (f *talkPosternFixture) show(id, dir string) (string, error) {
	f.t.Helper()
	var out strings.Builder
	err := application.TalkShow{
		Postern:       f.postern,
		Cipher:        apptest.NewFakeCipher(),
		Keys:          stubPosternKeys{pubKey: talkWaitMayorKey},
		GovernorKey:   talkWaitGovernorKey,
		AttachmentDir: dir,
		Out:           &out,
	}.Run(context.Background(), id)
	return out.String(), err
}

// mw talk show prints every turn of the talk, whole, with its role, turn,
// time, text and picture path, and none of another talk's.
func TestTalkShowPrintsEveryTurnOfTheTalkWhole(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	long := strings.Repeat("never cut ", 200)
	f.addTurnOf("direct:t1", "talk-9", 1, "first")
	f.addTurnOf("direct:other", "talk-8", 1, "somebody else's")
	f.addTurnOf("direct:t2", "talk-9", 2, long)
	f.addPictureTurn("direct:pic9", "see", "Shot.jpg", []byte("jpeg"))

	printed, err := f.show("talk-9", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"talk talk-9 turn 1 (role turn) at 2026-10-16T13:09:01Z\ntext: first\n",
		"talk talk-9 turn 2 (role turn) at 2026-10-16T13:09:02Z\ntext: " + long + "\n",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected %q, got:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "somebody else's") || strings.Contains(printed, "see") {
		t.Errorf("expected only talk-9's turns, got:\n%s", printed)
	}
}

// A turn's picture path is printed under its text, as the wait prints it.
func TestTalkShowPrintsATurnsPicturePath(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	f.addPictureTurn("direct:pic9", "see this", "Shot.jpg", []byte("jpeg"))

	printed, err := f.show("talk-12", dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := "text: see this\nimage: " + filepath.Join(dir, "direct-pic9.jpg") + " (Shot.jpg)\n"; !strings.Contains(printed, want) {
		t.Errorf("expected %q, got:\n%s", want, printed)
	}
}

// A talk with no turn the Mayor can read is said so, as an error.
func TestTalkShowRefusesATalkWithNoTurns(t *testing.T) {
	f := newTalkPosternFixture(t)
	f.addTurnOf("direct:t1", "talk-9", 1, "first")

	if _, err := f.show("talk-404", t.TempDir()); err == nil || !strings.Contains(err.Error(), "talk-404") {
		t.Errorf("expected an error naming talk-404, got %v", err)
	}
}

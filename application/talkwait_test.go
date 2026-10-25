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

// addFiles indexes a message from the Governor to the Mayor with the words
// text and files of the given mimes, and returns its seq. The wait never
// fetches a file it does not hear, so no blob is stored.
func (f *talkPosternFixture) addFiles(txid, text string, mimes ...string) int64 {
	f.t.Helper()
	var files []*application.PosternAttachment
	for n, mime := range mimes {
		files = append(files, &application.PosternAttachment{Hash: strings.Repeat("a", 60) + string(rune('a'+n)) + "bcd", Size: 10, Mime: mime})
	}
	msg := application.PosternThreadedMessage{Text: text}
	if len(files) == 1 {
		msg.Attachment = files[0]
	} else {
		msg.Attachments = files
	}
	plaintext := text
	if len(files) > 0 {
		body, _ := json.Marshal(msg)
		plaintext = string(body)
	}
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, plaintext)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq
}

// The notice of a message says what it holds beyond its first line: pictures
// it carries and lines it has, so the inbox is not skipped (mw-gq6.675).
func TestTalkWaitNoticeNamesPicturesAndFurtherLines(t *testing.T) {
	cases := []struct {
		name, text string
		mimes      []string
		want       string
	}{
		{"two pictures", "see these", []string{"image/jpeg", "image/jpeg"}, "see these [carries 2 pictures: mw postern inbox downloads them]"},
		{"one picture", "see this", []string{"image/jpeg"}, "see this [carries 1 picture: mw postern inbox downloads them]"},
		{"text only", "plain words", nil, "plain words"},
		{"voice note and a picture", "", []string{"audio/webm", "image/png"},
			voiceToFollow + " [carries 1 picture: mw postern inbox downloads them]"},
		{"three lines", "one\ntwo\nthree", nil, "one [+2 more lines]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newTalkPosternFixture(t)
			seq := f.addFiles("direct:n1", c.text, c.mimes...)
			_, printed := f.run(seq)
			want := "channel general, txid direct:n1: " + c.want + "\n"
			if !strings.Contains(printed, want) {
				t.Errorf("expected the line %q, got:\n%s", want, printed)
			}
		})
	}
}

const voiceToFollow = "voice note (words to follow by mail Voice: ...; read with mw postern inbox)"

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

// talkFile is a file a multi-file turn carries: its bytes, mime and name; gone
// leaves its blob out of the backend, as a file that cannot be fetched.
type talkFile struct {
	name, mime string
	body       []byte
	gone       bool
}

// addFilesTurn indexes a talk turn from the Governor to the Mayor carrying
// files as the `attachments` array Postern writes for two or more, and returns
// its seq.
func (f *talkPosternFixture) addFilesTurn(txid, text string, files []talkFile) int64 {
	f.t.Helper()
	var entries []map[string]any
	for _, file := range files {
		sealed, err := f.cipher.EncryptBytes(talkWaitMayorKey, file.body)
		if err != nil {
			f.t.Fatal(err)
		}
		raw, err := base64.StdEncoding.DecodeString(sealed)
		if err != nil {
			f.t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		if !file.gone {
			f.postern.SetBlob(hash, raw)
		}
		entries = append(entries, map[string]any{"hash": hash, "size": len(raw), "mime": file.mime, "name": file.name})
	}
	plain, _ := json.Marshal(map[string]any{
		"talk": map[string]any{"id": "talk-12", "turn": 1}, "text": text, "role": application.TalkRoleTurn,
		"attachments": entries,
	})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(plain))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "talk", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
	}).Seq
}

// A turn carrying `attachments` prints one line per file, each written under
// the turn's txid with its own index; a file that cannot be fetched is said
// and does not cost the others (mw-gq6.802).
func TestTalkWaitPrintsEveryFileOfATurnCarryingAttachments(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	seq := f.addFilesTurn("direct:many", "Look at these", []talkFile{
		{name: "one.jpg", mime: "image/jpeg", body: []byte("first")},
		{name: "two.png", mime: "image/png", body: []byte("second")},
		{name: "three.jpg", mime: "image/jpeg", body: []byte("third"), gone: true},
	})

	printed := f.runInto(seq, dir)

	one, two := filepath.Join(dir, "direct-many-1.jpg"), filepath.Join(dir, "direct-many-2.png")
	for _, want := range []string{"image: " + one + " (one.jpg)\n", "image: " + two + " (two.png)\n", "image: could not be fetched ("} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected %q, got:\n%s", want, printed)
		}
	}
	if strings.Count(printed, "image: ") != 3 {
		t.Errorf("expected three image lines, got:\n%s", printed)
	}
	for path, body := range map[string]string{one: "first", two: "second"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != body {
			t.Errorf("expected %q at %s, got %q (%v)", body, path, got, err)
		}
	}
}

// A file that is not a picture is named on a 'file:' line, not an 'image:' one.
func TestTalkWaitNamesANonPictureFileOfATurnAsAFile(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	seq := f.addFilesTurn("direct:mix", "Both", []talkFile{
		{name: "shot.png", mime: "image/png", body: []byte("png")},
		{name: "spec.pdf", mime: "application/pdf", body: []byte("pdf")},
	})

	printed := f.runInto(seq, dir)

	for _, want := range []string{
		"image: " + filepath.Join(dir, "direct-mix-1.png") + " (shot.png)\n",
		"file: " + filepath.Join(dir, "direct-mix-2.pdf") + " (spec.pdf)\n",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected %q, got:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "image: "+filepath.Join(dir, "direct-mix-2.pdf")) {
		t.Errorf("expected the PDF not to be called an image, got:\n%s", printed)
	}
}

// mw talk show lists every file of a turn the way the wait does.
func TestTalkShowListsEveryFileOfATurn(t *testing.T) {
	f := newTalkPosternFixture(t)
	dir := t.TempDir()
	f.addFilesTurn("direct:many", "Look at these", []talkFile{
		{name: "one.jpg", mime: "image/jpeg", body: []byte("first")},
		{name: "spec.pdf", mime: "application/pdf", body: []byte("pdf")},
	})

	printed, err := f.show("talk-12", dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "text: Look at these\nimage: " + filepath.Join(dir, "direct-many-1.jpg") + " (one.jpg)\nfile: " + filepath.Join(dir, "direct-many-2.pdf") + " (spec.pdf)\n"
	if !strings.Contains(printed, want) {
		t.Errorf("expected %q, got:\n%s", want, printed)
	}
}

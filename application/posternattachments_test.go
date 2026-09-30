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

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// manyFiles is one post carrying several files: the plaintext's `attachments`
// array, each entry's bytes kept to compare what was written.
type manyFiles struct {
	*attachmentFixture
	entries []application.PosternAttachment
	images  [][]byte
}

func newManyFiles(t *testing.T) *manyFiles {
	t.Helper()
	return &manyFiles{attachmentFixture: newAttachmentFixture(t)}
}

// add stores one more blob of content, announced under its own hash, and
// returns its entry.
func (f *manyFiles) add(t *testing.T, mime string, content []byte) application.PosternAttachment {
	t.Helper()
	sealed, err := f.cipher.Encrypt(attachmentInboxPubKey, string(content))
	if err != nil {
		t.Fatalf("encrypting a file: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decoding the fake ciphertext: %v", err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	f.backend.SetBlob(hash, raw)
	entry := application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: mime}
	f.entries = append(f.entries, entry)
	f.images = append(f.images, content)
	return entry
}

// post adds a record whose plaintext is body, a JSON object written by hand
// so that a body carrying both fields can be built.
func (f *manyFiles) post(t *testing.T, txid string, body map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("building the message: %v", err)
	}
	ciphertext, err := f.cipher.Encrypt(attachmentInboxPubKey, string(raw))
	if err != nil {
		t.Fatalf("encrypting the message: %v", err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: attachmentInboxPubKey, Ciphertext: ciphertext,
	})
}

func (f *manyFiles) assertWritten(t *testing.T, name string, want []byte) string {
	t.Helper()
	path := filepath.Join(f.dir, name)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected %s to be mode 0600, got %o", path, info.Mode().Perm())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("expected %s to hold the decrypted file, got %d bytes", path, len(got))
	}
	return path
}

var (
	pngBytes  = bytes.Repeat([]byte{0x89, 0x50, 0x4e, 0x47}, 300)
	jpegBytes = bytes.Repeat([]byte{0xff, 0xd8, 0xff, 0xe0}, 300)
)

// AC (a): a General message whose plaintext carries `attachments` has every
// file downloaded, checked and written 0600 as <name>-1.png, <name>-2.jpg, and
// each path printed under the message.
func TestInboxReadsAttachmentsOfOnePostAndNamesEachInOrder(t *testing.T) {
	f := newManyFiles(t)
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	f.post(t, "many-txid", map[string]any{"text": "two shots", "attachments": []any{one, two}})
	out := &bytes.Buffer{}

	messages, err := f.inbox(out).Run(context.Background())
	if err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	p1 := f.assertWritten(t, "many-txid-1.png", pngBytes)
	p2 := f.assertWritten(t, "many-txid-2.jpg", jpegBytes)
	printed := out.String()
	if !strings.Contains(printed, "two shots") {
		t.Errorf("expected the caption printed, got:\n%s", printed)
	}
	if !strings.Contains(printed, "\n"+p1+"\n") || !strings.Contains(printed, "\n"+p2+"\n") {
		t.Errorf("expected each path on its own line, got:\n%s", printed)
	}
	if strings.Index(printed, p1) > strings.Index(printed, p2) {
		t.Errorf("expected the paths in the array's order, got:\n%s", printed)
	}
}

// One entry in `attachments` keeps today's single name.
func TestInboxReadsAttachmentsWithOneEntryUnderTheSingleName(t *testing.T) {
	f := newManyFiles(t)
	one := f.add(t, "image/png", pngBytes)
	f.post(t, "solo-txid", map[string]any{"text": "one shot", "attachments": []any{one}})

	if _, err := f.inbox(&bytes.Buffer{}).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	f.assertWritten(t, "solo-txid.png", pngBytes)
}

// AC (b): in a bead's channel the post writes ONE bead comment naming both
// files, and the apply pass's result names them too.
func TestInboxNamesEveryOfTheAttachmentsInOneBeadComment(t *testing.T) {
	f := newManyFiles(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-many.1"})
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	f.post(t, "bead-txid", map[string]any{
		"thread": map[string]any{"bead": "mw-many.1"}, "text": "both", "attachments": []any{one, two},
	})
	out := &bytes.Buffer{}

	if _, err := f.inbox(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	p1 := f.assertWritten(t, "bead-txid-1.png", pngBytes)
	p2 := f.assertWritten(t, "bead-txid-2.jpg", jpegBytes)
	comments, err := f.tracker.StoryComments(context.Background(), "mw-many.1")
	if err != nil {
		t.Fatalf("reading the comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected ONE comment, got %d", len(comments))
	}
	want := " [image: " + p1 + "] [image: " + p2 + "]"
	if !strings.HasSuffix(comments[0].Text, want) {
		t.Errorf("expected the comment to end with %q, got %q", want, comments[0].Text)
	}
	for _, p := range []string{p1, p2} {
		if !strings.Contains(out.String(), p) {
			t.Errorf("expected %s printed, got:\n%s", p, out.String())
		}
	}
}

// The apply pass's result Detail names every file too.
func TestApplyDetailNamesEveryOfTheAttachments(t *testing.T) {
	f := newManyFiles(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-many.2"})
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	f.post(t, "apply-txid", map[string]any{
		"thread": map[string]any{"bead": "mw-many.2"}, "text": "both", "attachments": []any{one, two},
	})
	inbox := f.inbox(&bytes.Buffer{})
	inbox.Out = &bytes.Buffer{}
	inbox.Mailbox = apptest.NewFakeMailbox()

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	p1 := filepath.Join(f.dir, "apply-txid-1.png")
	p2 := filepath.Join(f.dir, "apply-txid-2.jpg")
	noted, err := f.tracker.Note(context.Background(), application.PosternAppliedKey("apply-txid"))
	if err != nil {
		t.Fatalf("reading the applied note: %v", err)
	}
	if !strings.Contains(noted, "[image: "+p1+"] [image: "+p2+"]") {
		t.Errorf("expected the result to name both files, got:\n%s", noted)
	}
}

// AC (c): a hash mismatch on the first entry is refused for it alone; the
// second is still written.
func TestInboxRefusesOneOfTheAttachmentsAndStillWritesTheOthers(t *testing.T) {
	f := newManyFiles(t)
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	wrong := hex.EncodeToString(sha256.New().Sum(nil))
	f.backend.SetBlob(wrong, []byte("not what the hash says"))
	one.Hash = wrong
	f.post(t, "mixed-txid", map[string]any{"text": "one bad", "attachments": []any{one, two}})
	out := &bytes.Buffer{}

	if _, err := f.inbox(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if !strings.Contains(out.String(), "attachment refused: hash mismatch") {
		t.Errorf("expected the refusal printed, got:\n%s", out.String())
	}
	f.assertWritten(t, "mixed-txid-2.jpg", jpegBytes)
	if _, err := os.Stat(filepath.Join(f.dir, "mixed-txid-1.png")); err == nil {
		t.Errorf("expected nothing written for the refused file")
	}
}

// AC (d): a body with both `attachment` and `attachments` reads `attachments`.
func TestInboxReadsAttachmentsWhenBothFieldsArePresent(t *testing.T) {
	f := newManyFiles(t)
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/jpeg", jpegBytes)
	stray := f.add(t, "image/webp", []byte("a stray webp"))
	f.post(t, "both-txid", map[string]any{"text": "both fields", "attachment": stray, "attachments": []any{one, two}})

	if _, err := f.inbox(&bytes.Buffer{}).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	f.assertWritten(t, "both-txid-1.png", pngBytes)
	f.assertWritten(t, "both-txid-2.jpg", jpegBytes)
	if _, err := os.Stat(filepath.Join(f.dir, "both-txid.webp")); err == nil {
		t.Errorf("expected the single attachment to be ignored when attachments is present")
	}
}

// A body whose only extra is `attachments` is a threaded body.
func TestAttachmentsAloneMakeAThreadedBody(t *testing.T) {
	f := newManyFiles(t)
	one := f.add(t, "image/png", pngBytes)
	two := f.add(t, "image/png", pngBytes)
	f.post(t, "bare-txid", map[string]any{"text": "", "attachments": []any{one, two}})

	messages, err := f.inbox(&bytes.Buffer{}).Run(context.Background())
	if err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if messages[0].Thread != application.PosternGeneralThread || len(messages[0].Attachments) != 2 {
		t.Errorf("expected a general post with 2 files, got thread %q and %d files", messages[0].Thread, len(messages[0].Attachments))
	}
}

// Marshalling writes `attachments` after `attachment`.
func TestThreadedMessageMarshalsAttachments(t *testing.T) {
	m := application.PosternThreadedMessage{
		Text:        "x",
		Attachments: []*application.PosternAttachment{{Hash: "h1", Size: 1, Mime: "image/png"}, {Hash: "h2", Size: 2, Mime: "image/jpeg"}},
		Re:          "direct:1",
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"text":"x","attachments":[{"hash":"h1","size":1,"mime":"image/png"},{"hash":"h2","size":2,"mime":"image/jpeg"}],"re":"direct:1"}`
	if string(raw) != want {
		t.Errorf("got %s, want %s", raw, want)
	}
}

// AC (e): an audio entry inside `attachments` is saved and printed, never
// transcribed, even on a host with a transcriber.
func TestInboxSavesAudioInAttachmentsWithoutTranscribingIt(t *testing.T) {
	f := newManyFiles(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-many.3"})
	audio := []byte("OggS pretend opus \x00\x01")
	one := f.add(t, "audio/ogg", audio)
	two := f.add(t, "image/png", pngBytes)
	f.post(t, "audio-txid", map[string]any{
		"thread": map[string]any{"bead": "mw-many.3"}, "text": "a note and a shot", "attachments": []any{one, two},
	})
	transcriber := &apptest.FakeTranscriber{Transcript: "should never be heard"}
	inbox := f.inbox(&bytes.Buffer{})
	out := &bytes.Buffer{}
	inbox.Out = out
	inbox.Transcriber = transcriber
	inbox.Mailbox = apptest.NewFakeMailbox()

	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	p1 := f.assertWritten(t, "audio-txid-1.ogg", audio)
	f.assertWritten(t, "audio-txid-2.png", pngBytes)
	if heard, _ := transcriber.Heard(); len(heard) != 0 {
		t.Errorf("expected nothing transcribed, heard %v", heard)
	}
	if !strings.Contains(out.String(), p1) {
		t.Errorf("expected the audio path printed, got:\n%s", out.String())
	}
	comments, _ := f.tracker.StoryComments(context.Background(), "mw-many.3")
	if len(comments) != 1 || !strings.Contains(comments[0].Text, "[audio: "+p1+"]") {
		t.Errorf("expected one comment naming the audio file, got %v", comments)
	}
}

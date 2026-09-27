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
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

const attachmentInboxPubKey = "attachment-inbox-pubkey-hex"

// attachmentFixture builds a Governor message carrying an attachment: the
// image bytes, encrypted with cipher to attachmentInboxPubKey exactly as
// PosternInbox.downloadAttachment expects to find them at Postern.Blob, and
// the message record itself, addressed to this key.
type attachmentFixture struct {
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	tracker *apptest.FakeTracker
	dir     string

	image []byte
	hash  string
	raw   []byte
}

func newAttachmentFixture(t *testing.T) *attachmentFixture {
	t.Helper()
	cipher := apptest.NewFakeCipher()
	cipher.From = releaseTapGovernorKey
	return &attachmentFixture{
		backend: apptest.NewFakePostern(),
		cipher:  cipher,
		tracker: apptest.NewFakeTracker(),
		dir:     filepath.Join(t.TempDir(), "postern", "inbox"),
		image:   bytes.Repeat([]byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x0d, 0x0a, 0x1a}, 400), // 3200 bytes, a stand-in PNG
	}
}

// blob encrypts f.image to attachmentInboxPubKey and stores the raw
// ciphertext bytes in the fake backend's blob store, exactly as
// POST /api/blobs would have, reporting the sha256 hash it is announced
// under.
func (f *attachmentFixture) blob(t *testing.T) string {
	t.Helper()
	ciphertextB64, err := f.cipher.Encrypt(attachmentInboxPubKey, string(f.image))
	if err != nil {
		t.Fatalf("encrypting the image: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		t.Fatalf("decoding the fake ciphertext: %v", err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	f.backend.SetBlob(hash, raw)
	f.hash = hash
	f.raw = raw
	return hash
}

// addMessage adds a record whose plaintext carries caption and an attachment
// announced under hash (mismatched, when it differs from f.hash), threaded on
// bead when it is not empty.
func (f *attachmentFixture) addMessage(t *testing.T, txid, caption, bead, hash string, size int64) {
	t.Helper()
	wrapped := application.PosternThreadedMessage{
		Text:       caption,
		Attachment: &application.PosternAttachment{Hash: hash, Size: size, Mime: "image/png"},
	}
	if bead != "" {
		wrapped.Thread = application.PosternThread{Bead: bead}
	}
	raw, err := json.Marshal(wrapped)
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

func (f *attachmentFixture) inbox(out *bytes.Buffer) application.PosternInbox {
	return application.PosternInbox{
		Postern:       f.backend,
		Cipher:        f.cipher,
		Keys:          stubPosternKeys{pubKey: attachmentInboxPubKey},
		Memory:        f.tracker,
		Tracker:       f.tracker,
		GovernorKey:   releaseTapGovernorKey,
		AttachmentDir: f.dir,
		Out:           out,
	}
}

// AC1: a message whose plaintext carries an attachment is downloaded, its
// hash checked, decrypted and written 0600 under AttachmentDir named by its
// txid and the mime's extension; its path is printed.
func TestInboxDownloadsDecryptsAndWritesAnAttachment(t *testing.T) {
	f := newAttachmentFixture(t)
	hash := f.blob(t)
	f.addMessage(t, "img-txid", "a screenshot", "", hash, int64(len(f.image)))
	out := &bytes.Buffer{}

	messages, err := f.inbox(out).Run(context.Background())
	if err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	wantPath := filepath.Join(f.dir, "img-txid.png")
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", wantPath, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected %s to be mode 0600, got %o", wantPath, info.Mode().Perm())
	}
	written, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading %s: %v", wantPath, err)
	}
	if !bytes.Equal(written, f.image) {
		t.Errorf("expected the written file to hold the decrypted image bytes, got %d bytes", len(written))
	}

	if !bytes.Contains(out.Bytes(), []byte(wantPath)) {
		t.Errorf("expected mw postern inbox to print %s, got:\n%s", wantPath, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("a screenshot")) {
		t.Errorf("expected mw postern inbox to print the caption, got:\n%s", out.String())
	}
}

// AC1 (threaded): the same download, on a message threaded to a bead the
// tracker knows, also ends the bead's comment with " [image: <path>]".
func TestInboxNamesTheAttachmentsPathInTheBeadComment(t *testing.T) {
	f := newAttachmentFixture(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-shot.1"})
	hash := f.blob(t)
	f.addMessage(t, "img-txid-2", "check this out", "mw-shot.1", hash, int64(len(f.image)))
	out := &bytes.Buffer{}

	if _, err := f.inbox(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	wantPath := filepath.Join(f.dir, "img-txid-2.png")
	comments, err := f.tracker.StoryComments(context.Background(), "mw-shot.1")
	if err != nil {
		t.Fatalf("reading mw-shot.1's comments: %v", err)
	}
	if len(comments) == 0 {
		t.Fatalf("expected a comment on mw-shot.1, found none")
	}
	last := comments[len(comments)-1].Text
	wantSuffix := " [image: " + wantPath + "]"
	if !bytes.HasSuffix([]byte(last), []byte(wantSuffix)) {
		t.Errorf("expected the comment to end with %q, got %q", wantSuffix, last)
	}
	if !bytes.Contains(out.Bytes(), []byte(wantPath)) {
		t.Errorf("expected mw postern inbox to also print %s, got:\n%s", wantPath, out.String())
	}
}

// AC2: a blob whose sha256 disagrees with the hash it was announced under is
// refused, writes nothing, and the message's text is still printed and
// recorded on the bead it names.
func TestInboxRefusesAnAttachmentWhoseHashDisagrees(t *testing.T) {
	f := newAttachmentFixture(t)
	f.tracker.AddStory("epic", domain.Story{ID: "mw-shot.2"})
	f.blob(t)
	wrongHash := hex.EncodeToString(sha256.New().Sum(nil)) // the empty string's sha256: never f.hash
	f.backend.SetBlob(wrongHash, f.raw)                    // reachable, but its own sha256 disagrees with wrongHash
	f.addMessage(t, "img-txid-3", "a mismatched screenshot", "mw-shot.2", wrongHash, int64(len(f.image)))
	out := &bytes.Buffer{}

	if _, err := f.inbox(out).Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("attachment refused: hash mismatch")) {
		t.Errorf("expected the refusal to be printed, got:\n%s", out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("a mismatched screenshot")) {
		t.Errorf("expected the caption to still be printed, got:\n%s", out.String())
	}
	if entries, err := os.ReadDir(f.dir); err == nil && len(entries) != 0 {
		t.Errorf("expected nothing written under %s, found %v", f.dir, entries)
	}

	comments, err := f.tracker.StoryComments(context.Background(), "mw-shot.2")
	if err != nil {
		t.Fatalf("reading mw-shot.2's comments: %v", err)
	}
	if len(comments) == 0 {
		t.Fatalf("expected the text to still be recorded on mw-shot.2, found no comment")
	}
	last := comments[len(comments)-1].Text
	if !bytes.Contains([]byte(last), []byte("a mismatched screenshot")) {
		t.Errorf("expected the recorded comment to carry the caption, got %q", last)
	}
	if bytes.Contains([]byte(last), []byte("[image:")) {
		t.Errorf("expected no image marker on a refused attachment, got %q", last)
	}
}

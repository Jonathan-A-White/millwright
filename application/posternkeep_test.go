package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const keepInboxKey = "keep-inbox-pubkey-hex"

// longMessage is 400 runes with a multi-byte letter in it, well past the 80
// the held line keeps.
var longMessage = strings.Repeat("é", 150) + strings.Repeat("plain words ", 20)

type keepFixture struct {
	tracker *apptest.FakeTracker
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	dir     string
	out     bytes.Buffer
}

func newKeepFixture(t *testing.T) *keepFixture {
	t.Helper()
	cipher := apptest.NewFakeCipher()
	cipher.From = releaseTapGovernorKey
	return &keepFixture{
		tracker: apptest.NewFakeTracker(), backend: apptest.NewFakePostern(), cipher: cipher,
		dir: filepath.Join(t.TempDir(), "postern", "inbox"),
	}
}

func (f *keepFixture) message(t *testing.T, txid, text string) {
	t.Helper()
	ct, err := f.cipher.Encrypt(keepInboxKey, text)
	mustDo(t, err)
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: keepInboxKey,
		Signer: releaseTapGovernorKey, Ts: time.Date(2026, 10, 16, 15, 6, 9, 0, time.UTC), Ciphertext: ct,
	})
}

func (f *keepFixture) inbox() application.PosternInbox {
	return application.PosternInbox{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: keepInboxKey},
		Memory: f.tracker, Tracker: f.tracker, Host: "desktop",
		GovernorKey: releaseTapGovernorKey, AttachmentDir: f.dir, Out: &f.out,
	}
}

// keptText reads back the text the message of txid was kept with.
func keptText(t *testing.T, dir, txid string) string {
	t.Helper()
	raw, err := os.ReadFile(application.PosternKeptFile(dir, txid))
	if err != nil {
		t.Fatalf("expected the message %s to be kept: %v", txid, err)
	}
	var kept struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &kept); err != nil {
		t.Fatalf("the kept message %s is not JSON: %v", txid, err)
	}
	return kept.Text
}

// AC1: the inbox keeps a message's whole text, 0600, written once.
func TestInboxKeepsTheWholeTextOfAMessage(t *testing.T) {
	f := newKeepFixture(t)
	f.message(t, "direct:long", longMessage)
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	path := application.PosternKeptFile(f.dir, "direct:long")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %o", info.Mode().Perm())
	}
	if got := keptText(t, f.dir, "direct:long"); got != longMessage {
		t.Errorf("the kept text differs from the message:\n got %q\nwant %q", got, longMessage)
	}
	before, err := os.ReadFile(path)
	mustDo(t, err)

	// A second read of the same txid (the cursor moved back) changes nothing,
	// even when the backend now says something else under that txid.
	mustDo(t, f.tracker.ClearNote(context.Background(), application.PosternCursorKey))
	f.backend = apptest.NewFakePostern()
	f.message(t, "direct:long", "something else entirely")
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	after, err := os.ReadFile(path)
	mustDo(t, err)
	if !bytes.Equal(before, after) {
		t.Errorf("the kept message was overwritten:\nbefore %s\n after %s", before, after)
	}
}

// AC1: --apply keeps the text too.
func TestApplyKeepsTheWholeTextOfAMessage(t *testing.T) {
	f := newKeepFixture(t)
	f.message(t, "direct:applied", longMessage)
	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if got := keptText(t, f.dir, "direct:applied"); got != longMessage {
		t.Errorf("the kept text differs: got %q", got)
	}
}

// memoryIdeasFiles is IdeasOnlyFiles kept in memory.
type memoryIdeasFiles struct {
	state application.IdeasOnlyState
	held  []application.IdeasOnlyHeld
}

func (m *memoryIdeasFiles) State(context.Context) (application.IdeasOnlyState, error) {
	return m.state, nil
}
func (m *memoryIdeasFiles) SetState(_ context.Context, s application.IdeasOnlyState) error {
	m.state = s
	return nil
}
func (m *memoryIdeasFiles) Held(context.Context) ([]application.IdeasOnlyHeld, error) {
	return m.held, nil
}
func (m *memoryIdeasFiles) Hold(_ context.Context, h application.IdeasOnlyHeld) error {
	m.held = append(m.held, h)
	return nil
}
func (m *memoryIdeasFiles) Release(context.Context, time.Time) error { return nil }

// AC2: --apply while Ideas only is on holds the message at 80 runes and still
// keeps the whole of it.
func TestApplyKeepsTheWholeTextOfAHeldMessage(t *testing.T) {
	f := newKeepFixture(t)
	now := time.Date(2026, 10, 16, 16, 0, 0, 0, time.UTC)
	f.backend.IdeasOnlyNow = application.IdeasOnlyNow{On: true, Until: now.Add(time.Hour).Unix(), By: application.IdeasOnlyByUntil}
	files := &memoryIdeasFiles{}
	inbox := f.inbox()
	inbox.Ideas = &application.IdeasOnlyMode{Reader: f.backend, Files: files, GovernorKey: releaseTapGovernorKey, Now: func() time.Time { return now }}
	f.message(t, "direct:idea", longMessage)

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if len(files.held) != 1 || len([]rune(files.held[0].Text)) != application.IdeasOnlyHeldWidth {
		t.Fatalf("expected one idea held at %d runes, got %+v", application.IdeasOnlyHeldWidth, files.held)
	}
	if got := keptText(t, f.dir, "direct:idea"); got != longMessage {
		t.Errorf("the kept text differs: got %q", got)
	}
}

// A voice note's transcript is text: kept whole beside the message.
func TestInboxKeepsAVoiceNotesTranscriptWhole(t *testing.T) {
	f := newKeepFixture(t)
	if err := application.KeepPosternTranscript(f.dir, "direct:voice", longMessage); err != nil {
		t.Fatalf("keeping the transcript: %v", err)
	}
	if err := application.KeepPosternTranscript(f.dir, "direct:voice", "not this"); err != nil {
		t.Fatalf("keeping it again: %v", err)
	}
	raw, err := os.ReadFile(application.PosternTranscriptFile(f.dir, "direct:voice"))
	mustDo(t, err)
	if string(raw) != longMessage {
		t.Errorf("expected the first transcript to stand, got %q", raw)
	}
}

// AC3: --show prints a past message whole and moves nothing.
func TestShowPrintsAKeptMessageWholeAndChangesNothing(t *testing.T) {
	f := newKeepFixture(t)
	f.message(t, "direct:long", longMessage)
	inbox := f.inbox()
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}
	cursor, err := f.tracker.Note(context.Background(), application.PosternCursorKey)
	mustDo(t, err)
	notes, err := f.tracker.NotesWithPrefix(context.Background(), "")
	mustDo(t, err)

	var out bytes.Buffer
	show := application.PosternInboxShow{Dir: f.dir, GovernorKey: releaseTapGovernorKey, Out: &out}
	if err := show.Run(context.Background(), "direct:long"); err != nil {
		t.Fatalf("showing: %v", err)
	}
	for _, want := range []string{"message", "from the Governor", "txid direct:long", "channel general", "2026-10-16", longMessage} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q in:\n%s", want, out.String())
		}
	}
	after, err := f.tracker.Note(context.Background(), application.PosternCursorKey)
	mustDo(t, err)
	notesAfter, err := f.tracker.NotesWithPrefix(context.Background(), "")
	mustDo(t, err)
	if after != cursor || len(notesAfter) != len(notes) {
		t.Errorf("--show changed the cursor or the notes: %q -> %q, %d -> %d notes", cursor, after, len(notes), len(notesAfter))
	}
}

// AC4: a txid with nothing kept is an error with a one-line reason.
func TestShowSaysSoWhenNothingIsKept(t *testing.T) {
	f := newKeepFixture(t)
	var out bytes.Buffer
	err := application.PosternInboxShow{Dir: f.dir, Out: &out}.Run(context.Background(), "direct:nothing")
	if err == nil {
		t.Fatal("expected an error for a txid with nothing kept")
	}
	if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "direct:nothing") {
		t.Errorf("expected a one-line reason naming the txid, got %q", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected nothing printed, got %q", out.String())
	}
	if err := (application.PosternInboxShow{Dir: f.dir, Out: &out}).Run(context.Background(), "../escape"); err == nil {
		t.Error("expected a txid that is a path to be refused")
	}
}

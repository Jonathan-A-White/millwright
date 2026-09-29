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

// stubPosternKeys is a minimal application.PosternKeyFile: PosternInbox only
// ever asks it for the public key (to filter records addressed to it) and a
// non-empty private key (FakeCipher.Decrypt does not care what it holds).
type stubPosternKeys struct{ pubKey string }

func (k stubPosternKeys) Path() string          { return "" }
func (k stubPosternKeys) Exists() (bool, error) { return true, nil }
func (k stubPosternKeys) Generate() error       { return nil }
func (k stubPosternKeys) PublicKey() (string, string, error) {
	return k.pubKey, "", nil
}
func (k stubPosternKeys) PrivateKeyWIF() (string, error) { return "priv", nil }
func (k stubPosternKeys) Sign(_ []application.PosternUtxo, _ []byte) (string, error) {
	return "", nil
}

func (k stubPosternKeys) MarkSpent(_ []application.PosternUtxo) error { return nil }

const (
	releaseTapInboxPubKey = "inbox-pubkey-hex"
	releaseTapGovernorKey = "governor-pubkey-hex"
)

// releaseTapFixture is an epic with two held stories, asked about with a
// decision-needed question already open and offering Release, so a test can
// exercise what a reply to it does.
type releaseTapFixture struct {
	tracker *apptest.FakeTracker
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	mailbox *apptest.FakeMailbox

	epicID         string
	story1, story2 string
}

func newReleaseTapFixture(t *testing.T) *releaseTapFixture {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	f := &releaseTapFixture{
		tracker: tracker,
		backend: apptest.NewFakePostern(),
		cipher:  apptest.NewFakeCipher(),
		mailbox: apptest.NewFakeMailbox(),
		epicID:  "mw-epic.1",
		story1:  "mw-epic.1.1",
		story2:  "mw-epic.1.2",
	}
	tracker.AddEpic(f.epicID, domain.Path{})
	tracker.AddStory(f.epicID, domain.Story{ID: f.story1, Title: "First"})
	tracker.AddStory(f.epicID, domain.Story{ID: f.story2, Title: "Second"})
	if err := tracker.SetStatus(f.story1, apptest.StatusDeferred); err != nil {
		t.Fatalf("holding %s: %v", f.story1, err)
	}
	if err := tracker.SetStatus(f.story2, apptest.StatusDeferred); err != nil {
		t.Fatalf("holding %s: %v", f.story2, err)
	}
	return f
}

// askQuestion marks f.epicID's question open under PosternQuestionKey,
// exactly as PosternSend.recordQuestion does: a txid and the options offered.
func (f *releaseTapFixture) askQuestion(t *testing.T, txid string, options ...string) {
	t.Helper()
	note, err := json.Marshal(struct {
		Txid    string   `json:"txid"`
		Options []string `json:"options,omitempty"`
	}{Txid: txid, Options: options})
	if err != nil {
		t.Fatalf("building the question note: %v", err)
	}
	if err := f.tracker.SetNote(context.Background(), application.PosternQuestionKey(f.epicID), string(note)); err != nil {
		t.Fatalf("marking the question open: %v", err)
	}
}

// reply adds a record answering f.epicID, verified as genuinely sent by from
// (the BRC-78 envelope's own sender, and the payload's claimed sender too).
func (f *releaseTapFixture) reply(t *testing.T, from, answer, txid string) {
	t.Helper()
	text, err := json.Marshal(application.PosternReply{Bead: f.epicID, Answer: answer})
	if err != nil {
		t.Fatalf("building the reply: %v", err)
	}
	f.cipher.From = from
	ciphertext, err := f.cipher.Encrypt(releaseTapInboxPubKey, string(text))
	if err != nil {
		t.Fatalf("encrypting the reply: %v", err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: from, To: releaseTapInboxPubKey, Ciphertext: ciphertext,
	})
}

func (f *releaseTapFixture) inbox() application.PosternInbox {
	return application.PosternInbox{
		Postern:     f.backend,
		Cipher:      f.cipher,
		Keys:        stubPosternKeys{pubKey: releaseTapInboxPubKey},
		Memory:      f.tracker,
		Tracker:     f.tracker,
		Mailbox:     f.mailbox,
		Host:        "vps",
		GovernorKey: releaseTapGovernorKey,
	}
}

func (f *releaseTapFixture) storyStatus(t *testing.T, id string) string {
	t.Helper()
	detail, err := f.tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatalf("reading %s: %v", id, err)
	}
	return detail.Status
}

func (f *releaseTapFixture) epicComments(t *testing.T) []string {
	t.Helper()
	comments, err := f.tracker.StoryComments(context.Background(), f.epicID)
	if err != nil {
		t.Fatalf("reading %s's comments: %v", f.epicID, err)
	}
	texts := make([]string, len(comments))
	for i, c := range comments {
		texts[i] = c.Text
	}
	return texts
}

func (f *releaseTapFixture) mailToMayor(t *testing.T) []application.Message {
	t.Helper()
	unread, err := f.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatalf("reading the Mayor's mailbox: %v", err)
	}
	return unread
}

func containsSubstring(texts []string, want string) bool {
	for _, text := range texts {
		if strings.Contains(text, want) {
			return true
		}
	}
	return false
}

// AC1: a Release tap from the Governor's own key, on a question that offered
// Release, releases the epic's held stories itself, exactly as `mw release`
// would, and mails the Mayor what happened.
func TestReleaseTapReleasesTheEpicsHeldStories(t *testing.T) {
	f := newReleaseTapFixture(t)
	f.askQuestion(t, "question-txid", "Release", "Hold")
	f.reply(t, releaseTapGovernorKey, "Release", "tap-txid")

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if got := f.storyStatus(t, f.story1); got != apptest.StatusOpen {
		t.Errorf("expected %s to be released (open), got %q", f.story1, got)
	}
	if got := f.storyStatus(t, f.story2); got != apptest.StatusOpen {
		t.Errorf("expected %s to be released (open), got %q", f.story2, got)
	}

	comments := f.epicComments(t)
	if !containsSubstring(comments, "RELEASED by mw on the Governor's Release tap (txid tap-txid)") {
		t.Errorf("expected a RELEASED comment naming the tap's txid, got %v", comments)
	}
	if !containsSubstring(comments, f.story1) || !containsSubstring(comments, f.story2) {
		t.Errorf("expected the RELEASED comment to name both stories now ready, got %v", comments)
	}

	mail := f.mailToMayor(t)
	found := false
	for _, m := range mail {
		if containsSubstring([]string{m.Subject}, "Released:") && containsSubstring([]string{m.Body}, "2 stories") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a mail to the Mayor about releasing 2 stories, got %+v", mail)
	}
}

// AC2 (Hold): any answer other than Release records as today and releases
// nothing.
func TestReleaseTapAnswerHoldReleasesNothing(t *testing.T) {
	f := newReleaseTapFixture(t)
	f.askQuestion(t, "question-txid", "Release", "Hold")
	f.reply(t, releaseTapGovernorKey, "Hold", "tap-txid")

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if got := f.storyStatus(t, f.story1); got != apptest.StatusDeferred {
		t.Errorf("expected %s to stay held, got %q", f.story1, got)
	}
	if got := f.storyStatus(t, f.story2); got != apptest.StatusDeferred {
		t.Errorf("expected %s to stay held, got %q", f.story2, got)
	}
	comments := f.epicComments(t)
	if containsSubstring(comments, "RELEASED") {
		t.Errorf("expected no RELEASED comment, got %v", comments)
	}
}

// AC2 (no Release option): a Release tap on a question that never offered
// Release releases nothing.
func TestReleaseTapQuestionWithoutReleaseOptionReleasesNothing(t *testing.T) {
	f := newReleaseTapFixture(t)
	f.askQuestion(t, "question-txid", "Hold", "Change")
	f.reply(t, releaseTapGovernorKey, "Release", "tap-txid")

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if got := f.storyStatus(t, f.story1); got != apptest.StatusDeferred {
		t.Errorf("expected %s to stay held, got %q", f.story1, got)
	}
	comments := f.epicComments(t)
	if containsSubstring(comments, "RELEASED") {
		t.Errorf("expected no RELEASED comment, got %v", comments)
	}
}

// AC2 (signer): a reply that is not from the Governor's own key — including
// when GovernorKey is not configured — records the answer but releases
// nothing, and the mail says the signer went unchecked.
func TestReleaseTapWrongSignerReleasesNothing(t *testing.T) {
	f := newReleaseTapFixture(t)
	f.askQuestion(t, "question-txid", "Release", "Hold")
	f.reply(t, "some-other-pubkey-hex", "Release", "tap-txid")

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if got := f.storyStatus(t, f.story1); got != apptest.StatusDeferred {
		t.Errorf("expected %s to stay held, got %q", f.story1, got)
	}
	comments := f.epicComments(t)
	if !containsSubstring(comments, "ANSWER") {
		t.Errorf("expected the answer to still be recorded, got %v", comments)
	}
	if containsSubstring(comments, "RELEASED") {
		t.Errorf("expected no RELEASED comment, got %v", comments)
	}

	mail := f.mailToMayor(t)
	found := false
	for _, m := range mail {
		if containsSubstring([]string{m.Body}, "signer unchecked") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a mail explaining the signer went unchecked, got %+v", mail)
	}
}

// AC2 (signer, empty GovernorKey): the same guard fires when no Governor key
// is configured at all.
func TestReleaseTapEmptyGovernorKeyReleasesNothing(t *testing.T) {
	f := newReleaseTapFixture(t)
	f.askQuestion(t, "question-txid", "Release", "Hold")
	f.reply(t, releaseTapGovernorKey, "Release", "tap-txid")

	inbox := f.inbox()
	inbox.GovernorKey = ""
	if _, err := inbox.Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if got := f.storyStatus(t, f.story1); got != apptest.StatusDeferred {
		t.Errorf("expected %s to stay held, got %q", f.story1, got)
	}
}

// AC2 (not an epic): a Release tap answering a bead that is a story, not an
// epic, releases nothing, and the mail says why.
func TestReleaseTapBeadNotAnEpicReleasesNothing(t *testing.T) {
	f := newReleaseTapFixture(t)
	// mw-epic.1.1 is a story, filed under mw-epic.1 — not an epic itself.
	note, err := json.Marshal(struct {
		Txid    string   `json:"txid"`
		Options []string `json:"options,omitempty"`
	}{Txid: "question-txid", Options: []string{"Release", "Hold"}})
	if err != nil {
		t.Fatalf("building the question note: %v", err)
	}
	if err := f.tracker.SetNote(context.Background(), application.PosternQuestionKey(f.story1), string(note)); err != nil {
		t.Fatalf("marking the question open: %v", err)
	}
	text, err := json.Marshal(application.PosternReply{Bead: f.story1, Answer: "Release"})
	if err != nil {
		t.Fatalf("building the reply: %v", err)
	}
	f.cipher.From = releaseTapGovernorKey
	ciphertext, err := f.cipher.Encrypt(releaseTapInboxPubKey, string(text))
	if err != nil {
		t.Fatalf("encrypting the reply: %v", err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: "tap-txid", Class: "message", From: releaseTapGovernorKey, To: releaseTapInboxPubKey, Ciphertext: ciphertext,
	})

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	if got := f.storyStatus(t, f.story1); got != apptest.StatusDeferred {
		t.Errorf("expected %s to stay held, got %q", f.story1, got)
	}

	mail := f.mailToMayor(t)
	found := false
	for _, m := range mail {
		if containsSubstring([]string{m.Body}, "not released") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a mail explaining why nothing was released, got %+v", mail)
	}
}

// AC2 (no held stories): a Release tap on an epic that holds nothing releases
// nothing, and the mail says why.
func TestReleaseTapEpicWithNoHeldStoriesReleasesNothing(t *testing.T) {
	f := newReleaseTapFixture(t)
	if err := f.tracker.ReleaseStory(context.Background(), f.story1); err != nil {
		t.Fatalf("releasing %s: %v", f.story1, err)
	}
	if err := f.tracker.ReleaseStory(context.Background(), f.story2); err != nil {
		t.Fatalf("releasing %s: %v", f.story2, err)
	}
	f.askQuestion(t, "question-txid", "Release", "Hold")
	f.reply(t, releaseTapGovernorKey, "Release", "tap-txid")

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("running the inbox: %v", err)
	}

	comments := f.epicComments(t)
	if containsSubstring(comments, "RELEASED") {
		t.Errorf("expected no RELEASED comment, got %v", comments)
	}
	mail := f.mailToMayor(t)
	found := false
	for _, m := range mail {
		if containsSubstring([]string{m.Body}, "no held stories") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a mail explaining there was nothing held, got %+v", mail)
	}
}

package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

const applyInboxKey = "apply-inbox-pubkey-hex"

// applyFixture is the Mayor's inbox over fakes: an epic with two held
// stories, an open one, a claimed one, and the Governor's key trusted.
type applyFixture struct {
	tracker *apptest.FakeTracker
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	mailbox *apptest.FakeMailbox
	out     bytes.Buffer
}

func newApplyFixture(t *testing.T) *applyFixture {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{})
	for _, id := range []string{"mw-e.1", "mw-e.2", "mw-e.3", "mw-e.4"} {
		tracker.AddStory("mw-e", domain.Story{ID: id, Title: "Story " + id})
	}
	mustDo(t, tracker.SetStatus("mw-e.1", apptest.StatusDeferred))
	mustDo(t, tracker.SetStatus("mw-e.2", apptest.StatusDeferred))
	mustDo(t, tracker.ClaimAs("mw-e.4", "mw@laptop", viewNow))
	cipher := apptest.NewFakeCipher()
	cipher.From = releaseTapGovernorKey
	return &applyFixture{tracker: tracker, backend: apptest.NewFakePostern(), cipher: cipher, mailbox: apptest.NewFakeMailbox()}
}

// message adds a record from `from`, verified as genuinely theirs, whose
// plaintext is text.
func (f *applyFixture) message(t *testing.T, from, txid, text string) {
	t.Helper()
	f.cipher.From = from
	ct, err := f.cipher.Encrypt(applyInboxKey, text)
	mustDo(t, err)
	f.backend.AddRecord(application.PosternRecord{Txid: txid, Class: "message", From: from, To: applyInboxKey, Signer: from, Ciphertext: ct})
	f.cipher.From = releaseTapGovernorKey
}

func (f *applyFixture) action(t *testing.T, txid string, action map[string]any) {
	t.Helper()
	raw, err := json.Marshal(action)
	mustDo(t, err)
	f.message(t, releaseTapGovernorKey, txid, string(raw))
}

func (f *applyFixture) inbox() application.PosternInbox {
	return application.PosternInbox{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: applyInboxKey},
		Memory: f.tracker, Tracker: f.tracker, Mailbox: f.mailbox, Host: "desktop",
		GovernorKey: releaseTapGovernorKey, Out: &f.out,
	}
}

func (f *applyFixture) apply(t *testing.T) {
	t.Helper()
	if _, err := f.inbox().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
}

func (f *applyFixture) status(t *testing.T, id string) string {
	t.Helper()
	d, err := f.tracker.ShowStory(context.Background(), id)
	mustDo(t, err)
	return d.Status
}

func (f *applyFixture) subjects(t *testing.T) []string {
	t.Helper()
	mail, err := f.mailbox.Inbox(context.Background(), application.MayorMailbox)
	mustDo(t, err)
	var out []string
	for _, m := range mail {
		out = append(out, m.Subject)
	}
	return out
}

// The Governor's release tap on a held story releases it at once, writes
// what was done on the bead, mails the Mayor, and marks the txid applied —
// without moving the inbox's cursor or printing what the message said.
func TestApplyReleasesAHeldStoryOnTheGovernorsTap(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-rel", map[string]any{"action": "release", "bead": "mw-e.1"})

	f.apply(t)

	if got := f.status(t, "mw-e.1"); got != apptest.StatusOpen {
		t.Fatalf("expected mw-e.1 released, got %s", got)
	}
	if got := f.tracker.Comments("mw-e.1"); len(got) != 1 || got[0] != "RELEASED by the Governor via postern, txid tx-rel" {
		t.Fatalf("expected the RELEASED comment, got %v", got)
	}
	if got := f.subjects(t); len(got) != 1 || got[0] != "Released: mw-e.1" {
		t.Fatalf("expected one mail to the Mayor, got %v", got)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-rel")); note != "applied release mw-e.1 txid tx-rel" {
		t.Fatalf("expected the txid marked applied, got %q", note)
	}
	if cursor, _ := f.tracker.Note(context.Background(), application.PosternCursorKey); cursor != "" {
		t.Fatalf("expected the cursor left where it was, got %q", cursor)
	}
	if strings.Contains(f.out.String(), `"action"`) {
		t.Fatalf("expected no message text printed, got %q", f.out.String())
	}
}

// A release of an epic releases every held story of it, as mw release does.
func TestApplyReleasesAnEpicsHeldStories(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-epic", map[string]any{"action": "release", "bead": "mw-e"})

	f.apply(t)

	for _, id := range []string{"mw-e.1", "mw-e.2"} {
		if got := f.status(t, id); got != apptest.StatusOpen {
			t.Fatalf("expected %s released, got %s", id, got)
		}
	}
	comments, _ := f.tracker.StoryComments(context.Background(), "mw-e")
	if len(comments) != 1 || !strings.HasPrefix(comments[0].Text, "RELEASED by the Governor via postern, txid tx-epic") {
		t.Fatalf("expected the RELEASED comment on the epic, got %+v", comments)
	}
}

// Hold holds an open, unclaimed story; one already claimed is refused, the
// Mayor told why, and the txid marked so it is never tried again.
func TestApplyHoldsAnOpenUnclaimedStoryAndRefusesAClaimedOne(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-hold", map[string]any{"action": "hold", "bead": "mw-e.3"})
	f.action(t, "tx-claimed", map[string]any{"action": "hold", "bead": "mw-e.4"})

	f.apply(t)

	if got := f.status(t, "mw-e.3"); got != apptest.StatusDeferred {
		t.Fatalf("expected mw-e.3 held, got %s", got)
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || got[0] != "HELD by the Governor via postern, txid tx-hold" {
		t.Fatalf("expected the HELD comment, got %v", got)
	}
	if got := f.status(t, "mw-e.4"); got != apptest.StatusInProgress {
		t.Fatalf("expected the claimed mw-e.4 left in progress, got %s", got)
	}
	if got := f.tracker.Comments("mw-e.4"); len(got) != 0 {
		t.Fatalf("expected no comment on a refused hold, got %v", got)
	}
	subjects := f.subjects(t)
	if len(subjects) != 2 || subjects[0] != "Held: mw-e.3" || subjects[1] != "Not applied: hold mw-e.4" {
		t.Fatalf("expected a Held mail and a Not applied mail, got %v", subjects)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-claimed")); !strings.HasPrefix(note, "refused hold mw-e.4 txid tx-claimed: ") {
		t.Fatalf("expected the refusal noted, got %q", note)
	}
}

func TestApplySetsAPriorityAndRefusesOneOutOfRange(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-p0", map[string]any{"action": "priority", "bead": "mw-e.3", "priority": 0})
	f.action(t, "tx-p9", map[string]any{"action": "priority", "bead": "mw-e.3", "priority": 9})
	f.action(t, "tx-none", map[string]any{"action": "priority", "bead": "mw-e.3"})

	f.apply(t)

	d, err := f.tracker.ShowStory(context.Background(), "mw-e.3")
	mustDo(t, err)
	if d.Priority != 0 {
		t.Fatalf("expected priority 0, got %d", d.Priority)
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || got[0] != "PRIORITY 0 set by the Governor via postern, txid tx-p0" {
		t.Fatalf("expected one PRIORITY comment, got %v", got)
	}
	if got := f.subjects(t); len(got) != 3 || got[1] != "Not applied: priority mw-e.3" || got[2] != "Not applied: priority mw-e.3" {
		t.Fatalf("expected the two bad priorities refused, got %v", got)
	}
}

func TestApplyCommentsVerifiedExactlyAsTheProtocolSays(t *testing.T) {
	f := newApplyFixture(t)
	mustDo(t, f.tracker.SetStatus("mw-e.3", apptest.StatusClosed))
	f.action(t, "direct:v1", map[string]any{"action": "verified", "bead": "mw-e.3"})

	f.apply(t)

	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || got[0] != "VERIFIED by the Governor via postern (direct:v1)" {
		t.Fatalf("expected the VERIFIED comment, got %v", got)
	}
}

// A second Verified tap on a bead already carrying a VERIFIED comment is
// refused, two taps being two transactions: nothing is written, and the
// Mayor is told it was not applied.
func TestApplyRefusesASecondVerified(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-first", map[string]any{"action": "verified", "bead": "mw-e.3"})
	f.action(t, "tx-second", map[string]any{"action": "verified", "bead": "mw-e.3"})

	f.apply(t)

	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || got[0] != "VERIFIED by the Governor via postern (tx-first)" {
		t.Fatalf("expected only the first VERIFIED comment, got %v", got)
	}
	got := f.subjects(t)
	if len(got) != 2 || got[0] != "Verified: mw-e.3" || got[1] != "Not applied: verified mw-e.3" {
		t.Fatalf("expected a Verified mail then a Not applied mail, got %v", got)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-second")); !strings.Contains(note, "it is already verified") {
		t.Fatalf("expected the second txid marked refused because it is already verified, got %q", note)
	}
}

// Each message is applied at most once, however many passes see it.
func TestApplyAppliesEachTxidOnce(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-once", map[string]any{"action": "verified", "bead": "mw-e.3"})

	f.apply(t)
	f.apply(t)

	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 {
		t.Fatalf("expected one comment after two passes, got %v", got)
	}
	if got := f.subjects(t); len(got) != 1 {
		t.Fatalf("expected one mail after two passes, got %v", got)
	}
}

// Only the Governor's verified word is applied; anything else, and an
// action the host does not know, is left for the Mayor to read.
func TestApplyLeavesAnythingButTheGovernorsKnownActionsAlone(t *testing.T) {
	f := newApplyFixture(t)
	raw, _ := json.Marshal(map[string]any{"action": "release", "bead": "mw-e.1"})
	f.message(t, "someone-else-pubkey-hex", "tx-other", string(raw))
	f.action(t, "tx-dance", map[string]any{"action": "dance", "bead": "mw-e.1"})

	f.apply(t)

	if got := f.status(t, "mw-e.1"); got != apptest.StatusDeferred {
		t.Fatalf("expected mw-e.1 still held, got %s", got)
	}
	for _, txid := range []string{"tx-other", "tx-dance"} {
		if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey(txid)); note != "" {
			t.Fatalf("expected %s left unapplied, got %q", txid, note)
		}
	}
	f.out.Reset()
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if !strings.Contains(f.out.String(), `"action":"dance"`) {
		t.Fatalf("expected the unknown action printed as text for the Mayor, got %q", f.out.String())
	}
}

// Apply records a reply and a bead-thread comment too, mailing the Mayor.
func TestApplyRecordsARepliesAndThreadComments(t *testing.T) {
	f := newApplyFixture(t)
	reply, _ := json.Marshal(application.PosternReply{Bead: "mw-e.3", Answer: "B"})
	f.message(t, releaseTapGovernorKey, "tx-answer", string(reply))
	threaded, _ := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: "mw-e.2"}, Text: "looks good"})
	f.message(t, releaseTapGovernorKey, "tx-comment", string(threaded))

	f.apply(t)

	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 || !strings.HasPrefix(got[0], "ANSWER ") {
		t.Fatalf("expected the ANSWER on mw-e.3, got %v", got)
	}
	if got := f.tracker.Comments("mw-e.2"); len(got) != 1 || !strings.HasSuffix(got[0], ": looks good") {
		t.Fatalf("expected the Governor's comment on mw-e.2, got %v", got)
	}
	subjects := f.subjects(t)
	if len(subjects) != 2 || subjects[0] != "Answer: mw-e.3: B" || !strings.HasPrefix(subjects[1], "Governor on mw-e.2") {
		t.Fatalf("expected an answer mail and a comment mail, got %v", subjects)
	}
}

// The Mayor's own read shows a message the pass already applied as one line,
// never its text, and never applies it again; it moves the cursor.
func TestInboxShowsAnAppliedMessageAsOneLineAndNeverAppliesItTwice(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-rel", map[string]any{"action": "release", "bead": "mw-e.1"})
	f.apply(t)
	f.out.Reset()

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := strings.TrimSpace(f.out.String()); got != "already applied earlier: applied release mw-e.1 txid tx-rel" {
		t.Fatalf("expected one line for the applied message, got %q", got)
	}
	if got := f.tracker.Comments("mw-e.1"); len(got) != 1 {
		t.Fatalf("expected the release written once, got %v", got)
	}
	if cursor, _ := f.tracker.Note(context.Background(), application.PosternCursorKey); cursor == "" {
		t.Fatal("expected the Mayor's read to move the cursor")
	}
}

// An answer the pass applied is printed by the Mayor's next read once, marked
// as old so it cannot be taken for the newest answer; the read after it does
// not meet it at all (mw-gq6.148).
func TestInboxPrintsAnAppliedAnswerOnceMarkedAsOld(t *testing.T) {
	f := newApplyFixture(t)
	reply, _ := json.Marshal(application.PosternReply{Bead: "mw-e.3", Answer: "B"})
	f.message(t, releaseTapGovernorKey, "tx-answer", string(reply))
	f.apply(t)
	f.out.Reset()

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if got := strings.TrimSpace(f.out.String()); got != "already applied earlier: applied answer mw-e.3 txid tx-answer" {
		t.Fatalf("expected the applied answer marked as old, once, got %q", got)
	}
	f.out.Reset()

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got := f.out.String(); got != "" {
		t.Fatalf("expected the second read to print nothing, got %q", got)
	}
}

// A read that fails after it printed an applied answer does not print the
// answer again in the read that follows: the cursor moved past it before the
// message that failed was tried.
func TestInboxThatFailsMidwayDoesNotPrintAnAppliedAnswerAgain(t *testing.T) {
	f := newApplyFixture(t)
	reply, _ := json.Marshal(application.PosternReply{Bead: "mw-e.3", Answer: "B"})
	f.message(t, releaseTapGovernorKey, "tx-answer", string(reply))
	f.apply(t)
	f.out.Reset()
	threaded, _ := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: "mw-e.2"}, Text: "looks good"})
	f.message(t, releaseTapGovernorKey, "tx-comment", string(threaded))
	f.mailbox.Err = errors.New("the mail store is locked")

	if _, err := f.inbox().Run(context.Background()); err == nil {
		t.Fatal("expected the first read to fail on the comment's mail")
	}
	if got := f.out.String(); !strings.Contains(got, "already applied earlier: applied answer mw-e.3 txid tx-answer") {
		t.Fatalf("expected the first read to print the answer marked as old, got %q", got)
	}
	f.out.Reset()
	f.mailbox.Err = nil

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got := f.out.String(); strings.Contains(got, "tx-answer") {
		t.Fatalf("expected the second read not to print the answer again, got %q", got)
	}
}

// With no pass run before it, the Mayor's read applies the action itself —
// once — and says so in one line.
func TestInboxAppliesAnActionThePassHasNotAndSaysSo(t *testing.T) {
	f := newApplyFixture(t)
	f.action(t, "tx-hold", map[string]any{"action": "hold", "bead": "mw-e.3"})

	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := f.status(t, "mw-e.3"); got != apptest.StatusDeferred {
		t.Fatalf("expected mw-e.3 held, got %s", got)
	}
	if got := strings.TrimSpace(f.out.String()); got != "applied hold mw-e.3 txid tx-hold" {
		t.Fatalf("expected the applied line, got %q", got)
	}
	f.apply(t)
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 {
		t.Fatalf("expected the hold written once, got %v", got)
	}
}

// BRC-78 carries no replay protection (postern's docs/protocol.md section
// 1): an action is applied as the Governor only when the backend vouched for
// who delivered or signed it. One whose signer is unchecked is left for the
// Mayor to read.
func TestApplyLeavesAnActionWhoseSignerIsUncheckedForTheMayor(t *testing.T) {
	f := newApplyFixture(t)
	raw, _ := json.Marshal(map[string]any{"action": "release", "bead": "mw-e.1"})
	ct, err := f.cipher.Encrypt(applyInboxKey, string(raw))
	mustDo(t, err)
	f.backend.AddRecord(application.PosternRecord{Txid: "tx-unchecked", Class: "message", From: releaseTapGovernorKey, To: applyInboxKey, Ciphertext: ct})

	f.apply(t)

	if got := f.status(t, "mw-e.1"); got != apptest.StatusDeferred {
		t.Fatalf("expected mw-e.1 still held, got %s", got)
	}
	f.out.Reset()
	if _, err := f.inbox().Run(context.Background()); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := f.status(t, "mw-e.1"); got != apptest.StatusDeferred || !strings.Contains(f.out.String(), `"action":"release"`) {
		t.Fatalf("expected the action left as text, unapplied, got status %s and %q", got, f.out.String())
	}
}

// A VERIFIED in the middle of a comment (a Builder's "VERIFIED by running")
// is no mark: the tap writes its comment. One beginning with VERIFIED, after
// white space, refuses it (mw-gq6.152).
func TestApplyVerifiedIsRefusedOnlyWhenACommentBeginsWithVERIFIED(t *testing.T) {
	ctx := context.Background()
	f := newApplyFixture(t)
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.3", "Done. VERIFIED by running: make test"))
	mustDo(t, f.tracker.CommentOnStory(ctx, "mw-e.4", " \nVERIFIED by the Mayor's check"))
	f.action(t, "tx-mid", map[string]any{"action": "verified", "bead": "mw-e.3"})
	f.action(t, "tx-begins", map[string]any{"action": "verified", "bead": "mw-e.4"})

	f.apply(t)

	if got := f.tracker.Comments("mw-e.3"); len(got) != 2 || got[1] != "VERIFIED by the Governor via postern (tx-mid)" {
		t.Fatalf("expected the tap's comment after the mid-text VERIFIED, got %v", got)
	}
	if got := f.tracker.Comments("mw-e.4"); len(got) != 1 {
		t.Fatalf("expected no comment written beside one beginning VERIFIED, got %v", got)
	}
	if note, _ := f.tracker.Note(ctx, application.PosternAppliedKey("tx-begins")); !strings.Contains(note, "it is already verified") {
		t.Fatalf("expected the refusal to say it is already verified, got %q", note)
	}
}

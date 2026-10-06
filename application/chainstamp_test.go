package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// stampKeys is a PosternKeyFile that signs anything it is given.
type stampKeys struct{ signed int }

func (k *stampKeys) Path() string          { return "" }
func (k *stampKeys) Exists() (bool, error) { return true, nil }
func (k *stampKeys) Generate() error       { return nil }
func (k *stampKeys) PublicKey() (string, string, error) {
	return stampTestSender, "mfactory-address", nil
}
func (k *stampKeys) PrivateKeyWIF() (string, error) { return "priv", nil }
func (k *stampKeys) Sign(_ []application.PosternUtxo, payload []byte) (string, error) {
	k.signed++
	return "rawtx:" + string(payload), nil
}
func (k *stampKeys) MarkSpent([]application.PosternUtxo) error { return nil }
func (k *stampKeys) MarkSent(string) error                     { return nil }

type chainStampFixture struct {
	queue   *apptest.FakeStampQueue
	backend *apptest.FakePostern
	tracker *apptest.FakeTracker
	keys    *stampKeys
	said    strings.Builder
}

func newChainStampFixture(stories ...string) *chainStampFixture {
	f := &chainStampFixture{
		queue:   apptest.NewFakeStampQueue(),
		backend: apptest.NewFakePostern(),
		tracker: apptest.NewFakeTracker(),
		keys:    &stampKeys{},
	}
	f.tracker.AddEpic("mw-epic", domain.Path{})
	for _, id := range stories {
		f.tracker.AddStory("mw-epic", domain.Story{ID: id, Title: "a story"})
	}
	return f
}

func (f *chainStampFixture) job() application.ChainStamp {
	cipher := apptest.NewFakeCipher()
	cipher.From = stampTestSender
	return application.ChainStamp{
		Queue: f.queue, Chain: apptest.NewFakeChain(f.backend, f.keys), Keys: f.keys, Cipher: cipher, Tracker: f.tracker,
		GovernorKey: stampTestKey,
		Now:         func() time.Time { return time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC) },
		Err:         &f.said,
	}
}

func stampFor(story, commit string) domain.Stamp {
	s := stampForTest()
	s.Story, s.Commit = story, commit
	return s
}

func TestChainStampBroadcastsOneTransactionPerPendingStampAndRecordsEachTxid(t *testing.T) {
	f := newChainStampFixture("mw-a.1", "mw-b.1")
	a, b := stampFor("mw-a.1", "aaaa"), stampFor("mw-b.1", "bbbb")
	f.queue.Append(context.Background(), a)
	f.queue.Append(context.Background(), b)

	if err := f.job().Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := len(f.backend.Broadcasts()); got != 2 {
		t.Fatalf("%d transactions broadcast, want 2", got)
	}
	if left := f.queue.Queued(); len(left) != 0 {
		t.Fatalf("still pending: %+v", left)
	}
	sent := f.queue.Sent()
	if len(sent) != 2 || sent[0].Txid != "fake-txid-1" || sent[1].Txid != "fake-txid-2" {
		t.Fatalf("sent = %+v", sent)
	}
	if sent[0].Stamp != a || sent[1].Stamp != b {
		t.Fatalf("sent stamps = %+v, %+v", sent[0].Stamp, sent[1].Stamp)
	}
	if got := f.tracker.Comments("mw-a.1"); len(got) != 1 || got[0] != "STAMP fake-txid-1 for aaaa (testnet)" {
		t.Fatalf("comments on mw-a.1 = %q", got)
	}
	if got := f.tracker.Comments("mw-b.1"); len(got) != 1 || got[0] != "STAMP fake-txid-2 for bbbb (testnet)" {
		t.Fatalf("comments on mw-b.1 = %q", got)
	}
}

func TestChainStampBroadcastsTheSealedPublicRecordAndNothingDirect(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	s := stampFor("mw-a.1", "aaaa")
	f.queue.Append(context.Background(), s)
	if err := f.job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw := strings.TrimPrefix(f.backend.Broadcasts()[0], "rawtx:")
	opened, _, err := application.OpenStamp(apptest.NewFakeCipher(), "priv", []byte(raw))
	if err != nil {
		t.Fatalf("what was broadcast is not a stamp: %v", err)
	}
	if opened.Rig != s.Rig || opened.Commit != s.Commit || opened.Story != s.Story {
		t.Fatalf("opened %+v, want %+v", opened, s)
	}
	if strings.Contains(raw[:strings.Index(raw, `"ct"`)], s.Rig) {
		t.Fatalf("the clear part names the rig: %s", raw)
	}
	if len(f.backend.Delivered()) != 0 {
		t.Fatal("a stamp was delivered directly: it goes on chain only")
	}
}

func TestChainStampLeavesAStampPendingWhenTheBackendFailsAndReturnsNil(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	f.backend.ChainErr = errors.New("backend down")

	if err := f.job().Run(context.Background()); err != nil {
		t.Fatalf("a failed broadcast must not fail the job, got %v", err)
	}
	left := f.queue.Queued()
	if len(left) != 1 || left[0].Attempts != 1 || !strings.Contains(left[0].LastError, "backend down") {
		t.Fatalf("pending = %+v, want the stamp with one failed try", left)
	}
	if len(f.queue.Sent()) != 0 || len(f.tracker.Comments("mw-a.1")) != 0 {
		t.Fatal("a stamp that was not broadcast was recorded as sent or commented")
	}
	if !strings.Contains(f.said.String(), "backend down") {
		t.Fatalf("the failure was not said: %q", f.said.String())
	}
}

func TestChainStampRetriesAStampOnTheNextTick(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	f.backend.ChainErr = errors.New("backend down")
	job := f.job()
	for i := 0; i < 2; i++ {
		if err := job.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if left := f.queue.Queued(); len(left) != 1 || left[0].Attempts != 2 {
		t.Fatalf("after two failed ticks pending = %+v", left)
	}

	f.backend.ChainErr = nil
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent := f.queue.Sent()
	if len(f.queue.Queued()) != 0 || len(sent) != 1 || sent[0].Attempts != 3 {
		t.Fatalf("after the third tick pending %+v, sent %+v", f.queue.Queued(), sent)
	}
	if got := f.tracker.Comments("mw-a.1"); len(got) != 1 {
		t.Fatalf("comments = %q", got)
	}
}

func TestChainStampOneStampFailingDoesNotStopTheOthers(t *testing.T) {
	f := newChainStampFixture("mw-a.1", "mw-b.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	f.queue.Append(context.Background(), stampFor("mw-b.1", "bbbb"))
	f.keys = &stampKeys{}
	job := f.job()
	job.Cipher = &failFirstCipher{FakeCipher: apptest.NewFakeCipher()}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if left := f.queue.Queued(); len(left) != 1 || left[0].Stamp.Story != "mw-a.1" {
		t.Fatalf("pending = %+v, want only mw-a.1", left)
	}
	if sent := f.queue.Sent(); len(sent) != 1 || sent[0].Stamp.Story != "mw-b.1" {
		t.Fatalf("sent = %+v, want only mw-b.1", sent)
	}
}

type failFirstCipher struct {
	*apptest.FakeCipher
	calls int
}

func (c *failFirstCipher) Encrypt(to, text string) (string, error) {
	c.calls++
	if c.calls == 1 {
		return "", errors.New("sealing failed")
	}
	return c.FakeCipher.Encrypt(to, text)
}

func TestChainStampWithoutAStoryBroadcastsAndComments(t *testing.T) {
	f := newChainStampFixture()
	s := stampFor("", "cccc")
	s.Rig = "vault"
	f.queue.Append(context.Background(), s)
	if err := f.job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sent := f.queue.Sent(); len(sent) != 1 || sent[0].Txid != "fake-txid-1" {
		t.Fatalf("sent = %+v", f.queue.Sent())
	}
}

func TestChainStampACommentThatFailsDoesNotUnsendTheStamp(t *testing.T) {
	f := newChainStampFixture() // the story is not in the tracker: the comment fails
	f.queue.Append(context.Background(), stampFor("mw-gone.1", "aaaa"))
	if err := f.job().Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.queue.Queued()) != 0 || len(f.queue.Sent()) != 1 {
		t.Fatalf("pending %+v sent %+v: a broadcast stamp must not be broadcast twice", f.queue.Queued(), f.queue.Sent())
	}
	if !strings.Contains(f.said.String(), "mw-gone.1") {
		t.Fatalf("the failed comment was not said: %q", f.said.String())
	}
}

func TestChainStampWithNothingPendingTouchesNothing(t *testing.T) {
	f := newChainStampFixture()
	if err := f.job().Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.backend.ChainCalls() != 0 {
		t.Fatal("the backend was called with nothing pending")
	}
}

func TestChainStampWithNoGovernorKeyFailsTheJobAndKeepsTheStamps(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	job := f.job()
	job.GovernorKey = ""
	if err := job.Run(context.Background()); err == nil {
		t.Fatal("a job that cannot seal anything said nothing")
	}
	if len(f.queue.Queued()) != 1 {
		t.Fatal("the stamp was lost")
	}
}

func TestChainStampJobIsAClockJobOnItsOwnName(t *testing.T) {
	job := application.ChainStampJob(time.Minute, func(context.Context) error { return nil })
	if job.Name != "chain-stamp" || job.Every != time.Minute || job.Wants != nil {
		t.Fatalf("job = %+v", job)
	}
}

func TestChainStampNotesTheTxidOnTheCommitInTheRigsCheckout(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	notes := apptest.NewFakeCommitNotes()
	job := f.job()
	job.Notes = notes
	job.Rigs = map[string]string{"millwright": "/rigs/millwright"}

	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := notes.Added()
	want := apptest.AddedNote{RigDir: "/rigs/millwright", Ref: "chain", Commit: "aaaa", Note: "fake-txid-1"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("notes = %+v, want [%+v]", got, want)
	}
}

func TestChainStampANoteThatFailsIsSaidAndDoesNotUnsendTheStamp(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	notes := apptest.NewFakeCommitNotes()
	notes.Err = errors.New("bad object aaaa")
	job := f.job()
	job.Notes = notes
	job.Rigs = map[string]string{"millwright": "/rigs/millwright"}

	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.queue.Sent()) != 1 || len(f.queue.Queued()) != 0 {
		t.Fatalf("pending %+v sent %+v: a note must not unsend the stamp", f.queue.Queued(), f.queue.Sent())
	}
	if !strings.Contains(f.said.String(), "bad object aaaa") {
		t.Fatalf("the failed note was not said: %q", f.said.String())
	}
	if got := f.tracker.Comments("mw-a.1"); len(got) != 1 {
		t.Fatalf("the story was not commented after a failed note: %q", got)
	}
}

func TestChainStampNotesNothingForARigWithNoCheckoutHere(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	notes := apptest.NewFakeCommitNotes()
	job := f.job()
	job.Notes = notes
	job.Rigs = map[string]string{"another": "/rigs/another"}

	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := notes.Added(); len(got) != 0 {
		t.Fatalf("a note was written for a rig with no checkout here: %+v", got)
	}
}

func TestChainStampNotesNothingForAFailedBroadcast(t *testing.T) {
	f := newChainStampFixture("mw-a.1")
	f.queue.Append(context.Background(), stampFor("mw-a.1", "aaaa"))
	f.backend.ChainErr = errors.New("backend down")
	notes := apptest.NewFakeCommitNotes()
	job := f.job()
	job.Notes = notes
	job.Rigs = map[string]string{"millwright": "/rigs/millwright"}

	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := notes.Added(); len(got) != 0 {
		t.Fatalf("a note for a stamp that was not sent: %+v", got)
	}
}

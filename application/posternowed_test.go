package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

var owedNow = time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)

const (
	owedHis1 = "direct:1111111111111111111111111111111111111111111111111111111111111111"
	owedHis2 = "direct:2222222222222222222222222222222222222222222222222222222222222222"
	owedHis3 = "direct:3333333333333333333333333333333333333333333333333333333333333333"
)

// owedFixture is the send fixture with his posts in the backend's index.
type owedFixture struct {
	*sendFixture
	at time.Time
}

func newOwedFixture() *owedFixture {
	return &owedFixture{sendFixture: newSendFixture(), at: owedNow}
}

func (f *owedFixture) sender(now time.Time) application.PosternSend {
	s := f.send("")
	s.Now = func() time.Time { return now }
	s.Threads = apptest.NewFakePosternThreadIndex()
	return s
}

// his adds one of his posts, written ago before owedNow, answering re when it
// names one.
func (f *owedFixture) his(t *testing.T, txid, text, re string, ago time.Duration) {
	t.Helper()
	body := text
	if re != "" {
		raw, err := json.Marshal(map[string]string{"text": text, "re": re})
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
	}
	f.cipher.From = releaseTapGovernorKey
	ct, err := f.cipher.Encrypt(sendMayorKey, body)
	f.cipher.From = sendMayorKey
	if err != nil {
		t.Fatal(err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: sendMayorKey,
		Ts: owedNow.Add(-ago), Ciphertext: ct,
	})
}

// ours adds a record of ours, as a send leaves in the index: sealed to him.
func (f *owedFixture) ours(t *testing.T, txid string, ago time.Duration) {
	t.Helper()
	ct, err := f.cipher.Encrypt(releaseTapGovernorKey, "whatever we said")
	if err != nil {
		t.Fatal(err)
	}
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: sendMayorKey, To: releaseTapGovernorKey,
		Ts: owedNow.Add(-ago), Ciphertext: ct,
	})
}

func (f *owedFixture) receipts(t *testing.T) map[string]string {
	t.Helper()
	kept, err := f.tracker.NotesWithPrefix(context.Background(), application.PosternReceiptKey(""))
	if err != nil {
		t.Fatal(err)
	}
	return kept
}

func (f *owedFixture) owed(t *testing.T) (string, application.PosternOwedReport) {
	t.Helper()
	var out bytes.Buffer
	owed := application.PosternOwed{
		Postern: f.backend, Cipher: f.cipher, Keys: stubPosternKeys{pubKey: sendMayorKey}, Notes: f.tracker,
		GovernorKey: releaseTapGovernorKey, Now: func() time.Time { return owedNow }, Out: &out,
	}
	report, err := owed.Run(context.Background())
	if err != nil {
		t.Fatalf("mw postern owed: %v", err)
	}
	return out.String(), report
}

// --receipt marks the post as a receipt for the message --re names, and a later
// post of ours that is not a receipt, --re into the same thread, clears it.
func TestAReceiptIsKeptUntilARealAnswerFollowsInTheSameThread(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "I'm saying duplication", "", 40*time.Minute)

	_, err := f.sender(owedNow.Add(-30*time.Minute)).Run(context.Background(),
		application.PosternSendRequest{Text: "Reading the picture...", Re: owedHis1, Receipt: true, InFactory: true})
	if err != nil {
		t.Fatal(err)
	}
	if kept := f.receipts(t); len(kept) != 1 {
		t.Fatalf("expected one receipt kept, got %v", kept)
	}
	if _, err := f.sender(owedNow).Run(context.Background(),
		application.PosternSendRequest{Text: "It is duplication; here is the count.", Re: owedHis1, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	if kept := f.receipts(t); len(kept) != 0 {
		t.Fatalf("expected the answer to clear the receipt, got %v", kept)
	}
}

// A receipt is cleared by a real answer to the root of its thread, as well as
// to the very post it receipted.
func TestARealAnswerToTheRootOfTheThreadClearsTheReceipt(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "first post", "", 90*time.Minute)
	f.his(t, owedHis2, "and another thing", owedHis1, 40*time.Minute)

	ctx := context.Background()
	if _, err := f.sender(owedNow.Add(-30*time.Minute)).Run(ctx,
		application.PosternSendRequest{Text: "Reading it.", Re: owedHis2, Receipt: true, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sender(owedNow).Run(ctx,
		application.PosternSendRequest{Text: "Both answered here.", Re: owedHis1, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	if kept := f.receipts(t); len(kept) != 0 {
		t.Fatalf("expected the answer to the root to clear the receipt, got %v", kept)
	}
}

// An answer in another thread, or a second receipt, clears nothing.
func TestAnAnswerElsewhereOrAnotherReceiptClearsNothing(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "Lagos post", "", 90*time.Minute)
	f.his(t, owedHis3, "Ruth post", "", 80*time.Minute)

	ctx := context.Background()
	if _, err := f.sender(owedNow.Add(-30*time.Minute)).Run(ctx,
		application.PosternSendRequest{Text: "Reading the picture.", Re: owedHis1, Receipt: true, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sender(owedNow).Run(ctx,
		application.PosternSendRequest{Text: "Here is the Ruth answer.", Re: owedHis3, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sender(owedNow).Run(ctx,
		application.PosternSendRequest{Text: "Still looking.", Re: owedHis1, Receipt: true, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	kept := f.receipts(t)
	if len(kept) != 1 {
		t.Fatalf("expected the one receipt still kept, got %v", kept)
	}
	for _, value := range kept {
		var receipt application.PosternReceipt
		if err := json.Unmarshal([]byte(value), &receipt); err != nil {
			t.Fatal(err)
		}
		if !receipt.At.Equal(owedNow.Add(-30 * time.Minute)) {
			t.Fatalf("expected the oldest receipt's time kept, got %v", receipt.At)
		}
	}
}

// A post of ours under 25 words that begins "Got it" is a receipt with no flag;
// a long one, or one that begins otherwise, is not; a transcript is not.
func TestAGotItPostUnder25WordsCountsAsAReceipt(t *testing.T) {
	long := "Got it " + strings.Repeat("and more ", 30)
	for name, c := range map[string]struct {
		text string
		role string
		want int
	}{
		"got it":              {text: "Got it; reading the picture...", want: 1},
		"got it lower case":   {text: "  got it, looking now", want: 1},
		"got it with a stop":  {text: "Got it.", want: 1},
		"over 25 words":       {text: long, want: 0},
		"another beginning":   {text: "Here is the answer, got it done", want: 0},
		"a word that follows": {text: "Got itself into a state", want: 0},
		"a transcript":        {text: "Got it", role: application.PosternRoleTranscript, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			f := newOwedFixture()
			f.his(t, owedHis1, "a post", "", 40*time.Minute)
			if _, err := f.sender(owedNow).Run(context.Background(),
				application.PosternSendRequest{Text: c.text, Re: owedHis1, Role: c.role, InFactory: true}); err != nil {
				t.Fatal(err)
			}
			if got := len(f.receipts(t)); got != c.want {
				t.Fatalf("expected %d receipts kept, got %d: %v", c.want, got, f.receipts(t))
			}
		})
	}
}

// --receipt names a post to receipt: with no --re it is refused.
func TestReceiptNeedsAPostToAnswer(t *testing.T) {
	err := application.PosternSendRequest{Text: "x", Receipt: true}.ValidateReplyFlags()
	if err == nil || !strings.Contains(err.Error(), "--receipt") || !strings.Contains(err.Error(), "--re") {
		t.Fatalf("expected --receipt refused with no --re, got %v", err)
	}
	if err := (application.PosternSendRequest{Text: "x", Receipt: true, Re: owedHis1}).ValidateReplyFlags(); err != nil {
		t.Fatalf("expected --receipt with --re accepted, got %v", err)
	}
}

// mw postern owed lists each post of his whose only reply is a receipt, or that
// has none, older than ten minutes.
func TestPosternOwedListsReceiptsAndPostsWithNoReplyOlderThanTenMinutes(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "I'm saying duplication also it seems like most of these are in Lagos not on the web", "", 90*time.Minute)
	f.his(t, owedHis2, "a fresh post nobody has had time to answer", "", 3*time.Minute)
	f.his(t, owedHis3, "an answered one", "", 80*time.Minute)
	f.ours(t, "direct:ours1", 85*time.Minute)
	f.ours(t, "direct:ours2", 20*time.Minute)

	// owedHis1 has a receipt and nothing more; owedHis3 has our later answer.
	if _, err := f.sender(owedNow.Add(-85*time.Minute)).Run(context.Background(),
		application.PosternSendRequest{Text: "Got it; reading the picture...", Re: owedHis1, InFactory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sender(owedNow.Add(-20*time.Minute)).Run(context.Background(),
		application.PosternSendRequest{Text: "The answer.", Re: owedHis3, InFactory: true}); err != nil {
		t.Fatal(err)
	}

	out, report := f.owed(t)
	if len(report.Owed) != 1 || report.Owed[0].Txid != owedHis1 || !report.Owed[0].OnlyReceipt {
		t.Fatalf("expected only the receipted post owed, got %+v", report.Owed)
	}
	for _, want := range []string{owedHis1, "1h30m", "I'm saying duplication also it seems like most of these are", "mw postern send --re " + owedHis1} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the listing:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Lagos") {
		t.Errorf("expected his words cut at 60 characters, got:\n%s", out)
	}
	for _, not := range []string{owedHis2, owedHis3} {
		if strings.Contains(out, not) {
			t.Errorf("did not expect %s listed:\n%s", not, out)
		}
	}
}

// A post of his with no record of ours after it at all is owed too.
func TestPosternOwedListsAPostWithNoReplyAtAll(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "anybody there", "", 25*time.Minute)

	out, report := f.owed(t)
	if len(report.Owed) != 1 || report.Owed[0].OnlyReceipt {
		t.Fatalf("expected the one post owed with no reply, got %+v", report.Owed)
	}
	if !strings.Contains(out, "no reply") || !strings.Contains(out, "25m") {
		t.Errorf("expected the listing to say no reply and the age, got:\n%s", out)
	}
}

// With nothing owed it says so and succeeds.
func TestPosternOwedSaysNothingOwedWhenClear(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "a post", "", 60*time.Minute)
	f.ours(t, "direct:ours1", 50*time.Minute)

	out, report := f.owed(t)
	if len(report.Owed) != 0 || strings.TrimSpace(out) != "nothing owed" {
		t.Fatalf("expected 'nothing owed', got %q and %+v", out, report.Owed)
	}
}

// mw status says it under DAMPERS: how many of his posts have only a receipt
// and how old the oldest is, once it is past the ten minutes.
func TestStatusDampersSaysHowManyOfHisPostsHaveOnlyAReceipt(t *testing.T) {
	f := newOwedFixture()
	f.his(t, owedHis1, "one", "", 90*time.Minute)
	f.his(t, owedHis2, "two", "", 60*time.Minute)
	f.his(t, owedHis3, "three", "", 2*time.Minute)
	ctx := context.Background()
	for re, at := range map[string]time.Duration{owedHis1: 25 * time.Minute, owedHis2: 15 * time.Minute, owedHis3: 1 * time.Minute} {
		if _, err := f.sender(owedNow.Add(-at)).Run(ctx,
			application.PosternSendRequest{Text: "Looking.", Re: re, Receipt: true, InFactory: true}); err != nil {
			t.Fatal(err)
		}
	}
	survey := application.DamperSurvey{Tracker: f.tracker, Notes: f.tracker}
	report, err := survey.Dampers(ctx, application.WorkInHand{}, owedNow)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	report.Write(&b)
	if want := "owed · 2 of his posts have only a receipt (oldest 25m): mw postern owed"; !strings.Contains(strings.Join(strings.Fields(b.String()), " "), want) {
		t.Fatalf("expected %q in the DAMPERS section, got:\n%s", want, b.String())
	}

	clear := newOwedFixture()
	report, err = application.DamperSurvey{Tracker: clear.tracker, Notes: clear.tracker}.Dampers(ctx, application.WorkInHand{}, owedNow)
	if err != nil {
		t.Fatal(err)
	}
	b.Reset()
	report.Write(&b)
	if strings.Contains(b.String(), "owed") {
		t.Fatalf("expected no owed line with nothing owed, got:\n%s", b.String())
	}
}

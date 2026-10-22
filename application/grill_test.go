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
	"github.com/Jonathan-A-White/millwright/domain"
)

const grillTicket = "mw-e.3"

// grillFixture is mw grill over fakes: the apply fixture's tracker with
// mw-e.3 labelled grilling, one backend for the decision cards and one for the
// running card.
type grillFixture struct {
	*applyFixture
	cards   *apptest.FakePostern
	cardOut bytes.Buffer
}

func newGrillFixture(t *testing.T) *grillFixture {
	t.Helper()
	f := &grillFixture{applyFixture: newApplyFixture(t), cards: apptest.NewFakePostern()}
	mustDo(t, f.tracker.SetLabels(grillTicket, "grilling", "wayfinder:map"))
	return f
}

func (f *grillFixture) cardSender() application.Cards {
	c := newCards(f.cards, &f.cardOut)
	c.Beads = f.tracker
	c.GovernorKey = releaseTapGovernorKey
	return c
}

func (f *grillFixture) grill() application.Grill {
	f.cipher.From = "mayor-key"
	defer func() { f.cipher.From = releaseTapGovernorKey }()
	return application.Grill{
		Send: application.PosternSend{
			Postern: f.backend, Chain: apptest.NewFakeChain(f.backend, stubPosternKeys{pubKey: "mayor-key"}),
			Cipher: f.cipher, Keys: stubPosternKeys{pubKey: "mayor-key"},
			Tracker: f.tracker, Notes: f.tracker,
			GovernorKey: releaseTapGovernorKey, FloatSats: 100000,
			Now: func() time.Time { return time.Unix(1790000000, 0) },
		},
		Cards:   f.cardSender(),
		Tracker: f.tracker,
		Notes:   f.tracker,
	}
}

func (f *grillFixture) ask(t *testing.T, question string) application.GrillAsked {
	t.Helper()
	asked, err := f.grill().Ask(context.Background(), application.GrillAsk{
		Bead: grillTicket, Question: question, Recommend: "A: yes", Options: []string{"A: yes", "B: no"},
	})
	if err != nil {
		t.Fatalf("mw grill: %v", err)
	}
	return asked
}

// cardRecord is the n-th record the running card's backend was handed: its
// class and its plaintext.
func (f *grillFixture) cardRecord(t *testing.T, n int) (string, string) {
	t.Helper()
	sent := f.cards.Delivered()
	if len(sent) <= n {
		t.Fatalf("expected at least %d card records, got %d", n+1, len(sent))
	}
	var payload application.PosternPayload
	mustDo(t, json.Unmarshal(sent[n], &payload))
	plain, _, err := apptest.NewFakeCipher().Decrypt("priv", payload.Ct)
	mustDo(t, err)
	return payload.Class, plain
}

func (f *grillFixture) inboxWithCards() application.PosternInbox {
	inbox := f.inbox()
	cards := f.cardSender()
	inbox.Cards = &cards
	return inbox
}

func (f *grillFixture) answer(t *testing.T, txid, re, answer string, at time.Time) {
	t.Helper()
	reply, _ := json.Marshal(application.PosternReply{Bead: grillTicket, Answer: answer, Re: re})
	ct, err := f.cipher.Encrypt(applyInboxKey, string(reply))
	mustDo(t, err)
	f.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: releaseTapGovernorKey, To: applyInboxKey,
		Signer: releaseTapGovernorKey, Ts: at, Ciphertext: ct,
	})
}

func TestGrillFirstQuestionSendsTheDecisionCardAndCreatesTheRunningCard(t *testing.T) {
	f := newGrillFixture(t)

	asked := f.ask(t, "Where does the second list live?")

	if asked.Question == "" || asked.Card == "" {
		t.Fatalf("expected a decision card txid and a running card txid, got %+v", asked)
	}
	if got := f.backend.Delivered(); len(got) != 1 {
		t.Fatalf("expected the decision card delivered, got %d records", len(got))
	}
	class, plain := f.cardRecord(t, 0)
	if class != application.CardClass {
		t.Fatalf("expected the running card sent as class card, got %q", class)
	}
	var card domain.Card
	mustDo(t, json.Unmarshal([]byte(plain), &card))
	if card.Thread == nil || card.Thread.Bead != grillTicket {
		t.Errorf("expected the card on %s's channel, got %+v", grillTicket, card.Thread)
	}
	if !strings.Contains(card.Title, grillTicket) {
		t.Errorf("expected the title to name the ticket, got %q", card.Title)
	}
	if len(card.Items) != 1 || card.Items[0].N != 1 {
		t.Fatalf("expected one item numbered 1, got %+v", card.Items)
	}
	item := card.Items[0]
	if !strings.Contains(item.Text, "Where does the second list live?") || !strings.Contains(item.Text, "open") ||
		!strings.Contains(item.Text, asked.Question) {
		t.Errorf("expected the item to say the question, that it is open and name its decision card, got %q", item.Text)
	}
	if len(item.Links) != 1 || item.Links[0] != grillTicket {
		t.Errorf("expected the item to link the ticket whose channel holds the decision card, got %v", item.Links)
	}
	note, err := f.tracker.Note(context.Background(), application.GrillKey(grillTicket))
	mustDo(t, err)
	if !strings.Contains(note, asked.Card) {
		t.Errorf("expected the ticket's metadata to keep the running card's txid %q, got %q", asked.Card, note)
	}
}

func TestGrillSecondQuestionUpdatesTheSameCard(t *testing.T) {
	f := newGrillFixture(t)
	first := f.ask(t, "Where does the second list live?")

	second := f.ask(t, "Who clears it?")

	if second.Card != first.Card {
		t.Errorf("expected the second question on the same card %q, got %q", first.Card, second.Card)
	}
	if got := f.cards.Delivered(); len(got) != 2 {
		t.Fatalf("expected the card sent once and updated once, got %d records", len(got))
	}
	class, plain := f.cardRecord(t, 1)
	if class != application.CardUpdateClass {
		t.Fatalf("expected a card update, got class %q", class)
	}
	var update domain.CardUpdate
	mustDo(t, json.Unmarshal([]byte(plain), &update))
	if update.Re != first.Card {
		t.Errorf("expected the update to name card %q, got %q", first.Card, update.Re)
	}
	if len(update.Items) != 1 || update.Items[0].N != 2 || !strings.Contains(update.Items[0].Text, "Who clears it?") {
		t.Errorf("expected item 2 for the second question, got %+v", update.Items)
	}
}

func TestGrillRefusesABeadThatIsNotAGrillingTicket(t *testing.T) {
	f := newGrillFixture(t)

	_, err := f.grill().Ask(context.Background(), application.GrillAsk{
		Bead: "mw-e.2", Question: "Why?", Recommend: "A", Options: []string{"A", "B"},
	})

	if err == nil || !strings.Contains(err.Error(), "grilling") {
		t.Fatalf("expected a refusal naming the grilling label, got %v", err)
	}
	if len(f.backend.Delivered()) != 0 || len(f.cards.Delivered()) != 0 {
		t.Errorf("expected nothing sent for a bead that is not a grilling ticket")
	}
}

func TestInboxApplyMarksTheGrillingItemAnsweredWithTheOptionAndTime(t *testing.T) {
	f := newGrillFixture(t)
	first := f.ask(t, "Where does the second list live?")
	f.ask(t, "Who clears it?")
	at := time.Date(2026, 10, 22, 3, 4, 5, 0, time.UTC)
	f.answer(t, "tx-ans", first.Question, "B: no", at)

	if _, err := f.inboxWithCards().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}

	if got := len(f.cards.Delivered()); got != 3 {
		t.Fatalf("expected the card, one update for the second question and one for the answer, got %d records", got)
	}
	class, plain := f.cardRecord(t, 2)
	if class != application.CardUpdateClass {
		t.Fatalf("expected a card update, got class %q", class)
	}
	var update domain.CardUpdate
	mustDo(t, json.Unmarshal([]byte(plain), &update))
	if update.Re != first.Card || len(update.Items) != 1 || update.Items[0].N != 1 {
		t.Fatalf("expected a replacement of item 1 of card %q, got %+v", first.Card, update)
	}
	text := update.Items[0].Text
	for _, want := range []string{"Where does the second list live?", "answered", "B: no", "2026-10-22T03:04:05Z"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected the item to say %q, got %q", want, text)
		}
	}
	if len(update.Tick) != 1 || update.Tick[0] != 1 {
		t.Errorf("expected item 1 ticked off, got %v", update.Tick)
	}

	// Another question still shows open, and a repeat of the answer changes nothing.
	f.answer(t, "tx-ans-again", first.Question, "A: yes", at.Add(time.Hour))
	if _, err := f.inboxWithCards().Apply(context.Background()); err != nil {
		t.Fatalf("applying again: %v", err)
	}
	if got := len(f.cards.Delivered()); got != 3 {
		t.Errorf("expected a second tap on an answered question to leave the card alone, got %d records", got)
	}
}

func TestInboxApplyOfAnAnswerWithoutAGrillingCardTouchesNoCard(t *testing.T) {
	f := newGrillFixture(t)
	f.answer(t, "tx-ans", "", "B: no", time.Date(2026, 10, 22, 3, 4, 5, 0, time.UTC))

	if _, err := f.inboxWithCards().Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}

	if got := len(f.cards.Delivered()); got != 0 {
		t.Errorf("expected no card record for a bead that has no running card, got %d", got)
	}
	if got := f.tracker.Comments(grillTicket); len(got) != 1 {
		t.Errorf("expected the answer recorded on the ticket as before, got %v", got)
	}
}

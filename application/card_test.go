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

func newCards(backend *apptest.FakePostern, out *bytes.Buffer) application.Cards {
	return application.Cards{
		Postern:     backend,
		Cipher:      &apptest.FakeCipher{From: "mayor-key"},
		Keys:        stubPosternKeys{pubKey: "mayor-key"},
		GovernorKey: "governor-key",
		FloatSats:   1000,
		Now:         func() time.Time { return time.Unix(1790000000, 0) },
		Out:         out,
	}
}

// sealedRecord is the one record backend was handed: its clear payload, and
// the plaintext the Governor opens.
func sealedRecord(t *testing.T, backend *apptest.FakePostern) (application.PosternPayload, string) {
	t.Helper()
	delivered := backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one record delivered, got %d", len(delivered))
	}
	if strings.Contains(string(delivered[0]), `"summary"`) {
		t.Errorf("expected a card record to carry no summary, got %s", delivered[0])
	}
	var payload application.PosternPayload
	if err := json.Unmarshal(delivered[0], &payload); err != nil {
		t.Fatal(err)
	}
	plain, from, err := apptest.NewFakeCipher().Decrypt("priv", payload.Ct)
	if err != nil {
		t.Fatal(err)
	}
	if payload.V != 1 || payload.Kind != application.PosternMessageKind || payload.To != "governor-key" ||
		payload.From != "mayor-key" || from != "mayor-key" || payload.Ts != 1790000000 {
		t.Errorf("expected a v1 msg record from the Mayor to the Governor at the clock's time, got %+v (sealed by %q)", payload, from)
	}
	return payload, plain
}

func TestCardSendPostsASealedCardRecordWithItemsLinksExpectationsAndSubscribe(t *testing.T) {
	backend := apptest.NewFakePostern()
	var out bytes.Buffer
	card, txid, err := newCards(backend, &out).Send(context.Background(), application.CardSendRequest{
		Title:       "Top 5",
		Items:       []string{"Approve mw-a.1: the plan|mw-a.1|", "Check the map|mw-b,mw-c|mw-b:landed", "Read the brief"},
		BeadChannel: "mw-x",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, plain := sealedRecord(t, backend)
	if payload.Class != application.CardClass || application.CardClass != "card" {
		t.Errorf("expected class card, got %q", payload.Class)
	}
	if card.ID != txid || txid == "" {
		t.Errorf("expected the card's ID to be its txid %q, got %q", txid, card.ID)
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); first != txid {
		t.Errorf("expected the first line printed to be the txid %q, got:\n%s", txid, out.String())
	}
	if !strings.Contains(out.String(), "2. Check the map  [mw-b, mw-c]  expects mw-b landed") {
		t.Errorf("expected each item printed with its number, links and expectation, got:\n%s", out.String())
	}

	var sealed domain.Card
	if err := json.Unmarshal([]byte(plain), &sealed); err != nil {
		t.Fatalf("the plaintext %q is not a card: %v", plain, err)
	}
	if sealed.Title != "Top 5" || sealed.Thread == nil || sealed.Thread.Bead != "mw-x" || len(sealed.Items) != 3 {
		t.Fatalf("expected the card Top 5 in mw-x's channel with three items, got %+v", sealed)
	}
	approve, check, read := sealed.Items[0], sealed.Items[1], sealed.Items[2]
	if approve.N != 1 || approve.Expect == nil || *approve.Expect != (domain.Expectation{Bead: "mw-a.1", State: domain.ExpectAnswered}) {
		t.Errorf("expected item 1 to expect mw-a.1 answered, derived from its ask, got %+v", approve)
	}
	if check.N != 2 || strings.Join(check.Links, ",") != "mw-b,mw-c" || check.Expect == nil || *check.Expect != (domain.Expectation{Bead: "mw-b", State: domain.ExpectLanded}) {
		t.Errorf("expected item 2 linking mw-b and mw-c and expecting mw-b landed, got %+v", check)
	}
	if read.N != 3 || read.Expect != nil || len(read.Links) != 0 {
		t.Errorf("expected item 3 with no links and no expectation, got %+v", read)
	}
	if got := strings.Join(sealed.Subscribe.Kinds, " "); got != "card_answered bead_changed" {
		t.Errorf("expected it to subscribe to card_answered and bead_changed, got %q", got)
	}
	if got := strings.Join(sealed.Subscribe.Beads, " "); got != "mw-a.1 mw-b mw-c" {
		t.Errorf("expected it to subscribe to mw-a.1 mw-b mw-c, got %q", got)
	}
}

func TestCardSendRefusesABadExpectationStateAndSendsNothing(t *testing.T) {
	backend := apptest.NewFakePostern()
	_, _, err := newCards(backend, &bytes.Buffer{}).Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"Check it|mw-a|mw-a:done"},
	})
	if err == nil || !strings.Contains(err.Error(), "mw card send:") || !strings.Contains(err.Error(), "open, landed, verified, closed, answered or held") {
		t.Fatalf("expected the state refused, naming the states, got %v", err)
	}
	if n := len(backend.Delivered()); n != 0 {
		t.Fatalf("expected nothing delivered, got %d", n)
	}
}

func TestCardSendRefusesWithNoGovernorKey(t *testing.T) {
	backend := apptest.NewFakePostern()
	cards := newCards(backend, &bytes.Buffer{})
	cards.GovernorKey = ""
	_, _, err := cards.Send(context.Background(), application.CardSendRequest{Title: "Top 5", Items: []string{"Read it"}})
	if err == nil || !strings.Contains(err.Error(), "postern_governor_key") {
		t.Fatalf("expected a refusal naming postern_governor_key, got %v", err)
	}
}

func TestCardSendOnTheChainKeepsToTheFloatCap(t *testing.T) {
	backend := apptest.NewFakePostern()
	backend.SetBalance("", 5000)
	cards := newCards(backend, &bytes.Buffer{})
	cards.Channel = application.PosternChannelChain
	_, _, err := cards.Send(context.Background(), application.CardSendRequest{Title: "Top 5", Items: []string{"Read it"}})
	if err == nil || !strings.Contains(err.Error(), "float cap of 1000") {
		t.Fatalf("expected the float cap to refuse it, got %v", err)
	}
	if n := len(backend.Broadcasts()); n != 0 {
		t.Fatalf("expected nothing broadcast, got %d", n)
	}
}

func TestCardUpdatePostsACardUpdateRecordWithRe(t *testing.T) {
	backend := apptest.NewFakePostern()
	var out bytes.Buffer
	update, txid, err := newCards(backend, &out).Update(context.Background(), application.CardUpdateRequest{
		Re: "direct:abc", Items: []string{"6. Release mw-f|mw-f|"}, Links: []string{"2:mw-b.1"}, Tick: []int{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, plain := sealedRecord(t, backend)
	if payload.Class != application.CardUpdateClass || application.CardUpdateClass != "card-update" {
		t.Errorf("expected class card-update, got %q", payload.Class)
	}
	if strings.TrimSpace(out.String()) != txid || update.Re != "direct:abc" {
		t.Errorf("expected the txid printed and re kept, got %q printed and %+v", out.String(), update)
	}
	var sealed domain.CardUpdate
	if err := json.Unmarshal([]byte(plain), &sealed); err != nil {
		t.Fatalf("the plaintext %q is not a card update: %v", plain, err)
	}
	if sealed.Re != "direct:abc" || len(sealed.Items) != 1 || sealed.Items[0].N != 6 ||
		sealed.Items[0].Expect == nil || *sealed.Items[0].Expect != (domain.Expectation{Bead: "mw-f", State: domain.ExpectOpen}) ||
		strings.Join(sealed.Links[2], ",") != "mw-b.1" || len(sealed.Tick) != 1 || sealed.Tick[0] != 1 {
		t.Fatalf("expected re direct:abc, item 6 expecting mw-f open, item 2 linked to mw-b.1 and item 1 ticked, got %+v", sealed)
	}
}

func TestCardUpdateRefusesNoCardAndSendsNothing(t *testing.T) {
	backend := apptest.NewFakePostern()
	_, _, err := newCards(backend, &bytes.Buffer{}).Update(context.Background(), application.CardUpdateRequest{Tick: []int{1}})
	if err == nil || !strings.Contains(err.Error(), "mw card update:") || !strings.Contains(err.Error(), "which card") {
		t.Fatalf("expected an update with no card refused, got %v", err)
	}
	if n := len(backend.Delivered()); n != 0 {
		t.Fatalf("expected nothing delivered, got %d", n)
	}
}

func TestPromptCardRecordsThePromptsNameOnTheCard(t *testing.T) {
	prompts := apptest.NewFakePrompts()
	if err := prompts.Put(context.Background(), domain.Prompt{Name: "top5", Summary: "x", Signature: []string{}, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	backend := apptest.NewFakePostern()
	card, _, err := application.PromptCard{Prompts: prompts, Cards: newCards(backend, &bytes.Buffer{})}.Run(
		context.Background(), "top5", application.CardSendRequest{Title: "Top 5", Items: []string{"Approve mw-a"}})
	if err != nil {
		t.Fatal(err)
	}
	_, plain := sealedRecord(t, backend)
	var sealed domain.Card
	if err := json.Unmarshal([]byte(plain), &sealed); err != nil {
		t.Fatal(err)
	}
	if sealed.Prompt != "top5" || card.Prompt != "top5" {
		t.Fatalf("expected the card to record the prompt top5, got %+v", sealed)
	}

	_, _, err = application.PromptCard{Prompts: prompts, Cards: newCards(apptest.NewFakePostern(), &bytes.Buffer{})}.Run(
		context.Background(), "nope", application.CardSendRequest{Title: "Top 5", Items: []string{"Approve mw-a"}})
	if err == nil || !strings.Contains(err.Error(), "no saved prompt /nope") {
		t.Fatalf("expected a prompt that is not saved refused, got %v", err)
	}
}

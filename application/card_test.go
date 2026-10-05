package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// cardBeads is the tracker a card is checked against: the epic mw-epic and the
// stories mw-a, mw-a.1, mw-b, mw-c and mw-f under it.
func cardBeads() *apptest.FakeTracker {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-epic", domain.Path{})
	for _, id := range []string{"mw-a", "mw-a.1", "mw-b", "mw-c", "mw-f"} {
		tracker.AddStory("mw-epic", domain.Story{ID: id, Title: id})
	}
	return tracker
}

func newCards(backend *apptest.FakePostern, out *bytes.Buffer) application.Cards {
	return application.Cards{
		Beads:       cardBeads(),
		Log:         apptest.NewFakeCardLog(),
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

func TestCardSendRefusesAnEpicExpectedLandedOrVerifiedAndSendsNothing(t *testing.T) {
	for _, state := range []string{"verified", "landed"} {
		backend := apptest.NewFakePostern()
		cards := newCards(backend, &bytes.Buffer{})
		_, _, err := cards.Send(context.Background(), application.CardSendRequest{
			Title: "Top 5", Items: []string{"Read it|mw-epic|mw-epic:" + state},
		})
		for _, want := range []string{"mw card send:", "mw-epic", "epic", "expect closed"} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %s refused saying %q, got %v", state, want, err)
			}
		}
		if n := len(backend.Delivered()); n != 0 {
			t.Fatalf("expected nothing delivered, got %d", n)
		}
		if records, _ := cards.Log.List(context.Background()); len(records) != 0 {
			t.Fatalf("expected nothing recorded, got %+v", records)
		}
	}
}

func TestCardSendRefusesAnEpicVerifiedByItsAskToo(t *testing.T) {
	_, _, err := newCards(apptest.NewFakePostern(), &bytes.Buffer{}).Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"VERIFIED on mw-epic"},
	})
	if err == nil || !strings.Contains(err.Error(), "expect closed") {
		t.Fatalf("expected the derived expectation refused, got %v", err)
	}
}

func TestCardSendAcceptsAnEpicClosedAndAStoryVerified(t *testing.T) {
	backend := apptest.NewFakePostern()
	_, _, err := newCards(backend, &bytes.Buffer{}).Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"Close it|mw-epic|mw-epic:closed", "Check it|mw-a|mw-a:verified", "Land it|mw-b|mw-b:landed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(backend.Delivered()); n != 1 {
		t.Fatalf("expected one record delivered, got %d", n)
	}
}

func TestCardSendRefusesABeadItCannotRead(t *testing.T) {
	backend := apptest.NewFakePostern()
	cards := newCards(backend, &bytes.Buffer{})
	_, _, err := cards.Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"Check it|mw-nope|mw-nope:verified"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot read mw-nope") {
		t.Fatalf("expected a bead that is not there refused, got %v", err)
	}

	tracker := cardBeads()
	tracker.Err = errors.New("bd is down")
	cards.Beads = tracker
	_, _, err = cards.Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"Check it|mw-a|mw-a:verified"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot read mw-a") || !strings.Contains(err.Error(), "bd is down") {
		t.Fatalf("expected a bead the tracker fails on refused, got %v", err)
	}
	if n := len(backend.Delivered()); n != 0 {
		t.Fatalf("expected nothing delivered, got %d", n)
	}
}

func TestCardSendReadsNoBeadForAnExpectationThatCanTick(t *testing.T) {
	cards := newCards(apptest.NewFakePostern(), &bytes.Buffer{})
	cards.Beads = nil
	_, _, err := cards.Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"Close it|mw-epic|mw-epic:closed", "Approve mw-q"},
	})
	if err != nil {
		t.Fatalf("expected closed and answered to need no read of the bead, got %v", err)
	}
}

func TestCardUpdateAndPromptCardRefuseAnEpicVerified(t *testing.T) {
	backend := apptest.NewFakePostern()
	cards := newCards(backend, &bytes.Buffer{})
	_, _, err := cards.Update(context.Background(), application.CardUpdateRequest{
		Re: "direct:abc", Items: []string{"3. Read it|mw-epic|mw-epic:verified"},
	})
	if err == nil || !strings.Contains(err.Error(), "mw card update:") || !strings.Contains(err.Error(), "expect closed") {
		t.Fatalf("expected the update refused, got %v", err)
	}

	prompts := apptest.NewFakePrompts()
	if err := prompts.Put(context.Background(), domain.Prompt{Name: "top5", Summary: "x", Signature: []string{}, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	_, _, err = application.PromptCard{Prompts: prompts, Cards: cards}.Run(context.Background(), "top5",
		application.CardSendRequest{Title: "Top 5", Items: []string{"Read it|mw-epic|mw-epic:landed"}})
	if err == nil || !strings.Contains(err.Error(), "mw prompt run --card:") || !strings.Contains(err.Error(), "expect closed") {
		t.Fatalf("expected the prompt's card refused, got %v", err)
	}
	if n := len(backend.Delivered()); n != 0 {
		t.Fatalf("expected nothing delivered, got %d", n)
	}
}

func TestCardsKeepEverySentCardAndListThemNewestFirst(t *testing.T) {
	backend := apptest.NewFakePostern()
	cards := newCards(backend, &bytes.Buffer{})
	clock := time.Unix(1790000000, 0)
	cards.Now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	_, first, err := cards.Send(context.Background(), application.CardSendRequest{
		Title: "Top 5", Items: []string{"Check it|mw-a|mw-a:verified", "Read it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := cards.Update(context.Background(), application.CardUpdateRequest{Re: first, Items: []string{"3. Release mw-f|mw-f|"}, Tick: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	_, third, err := cards.Send(context.Background(), application.CardSendRequest{Title: "Later", Items: []string{"Look"}})
	if err != nil {
		t.Fatal(err)
	}

	records, err := cards.Log.List(context.Background())
	if err != nil || len(records) != 3 {
		t.Fatalf("expected three records kept, got %+v (%v)", records, err)
	}
	if records[0].Txid != first || records[0].Title != "Top 5" || len(records[0].Items) != 2 ||
		records[0].Items[0].Expect == nil || records[0].Items[0].Expect.Bead != "mw-a" {
		t.Errorf("expected the first card kept with its title and items, got %+v", records[0])
	}
	if records[1].Txid != second || records[1].Re != first || len(records[1].Tick) != 1 || len(records[1].Items) != 1 {
		t.Errorf("expected the update kept naming its card, got %+v", records[1])
	}

	var out bytes.Buffer
	cards.Out = &out
	if err := cards.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	listed := out.String()
	at3, at1 := strings.Index(listed, third), strings.Index(listed, first)
	if at3 < 0 || at1 < 0 || at3 > at1 {
		t.Fatalf("expected the newest card listed first, got:\n%s", listed)
	}
	for _, want := range []string{"Top 5", "Later", "1. Check it", "expects mw-a verified", "update of " + first, "ticks 1"} {
		if !strings.Contains(listed, want) {
			t.Errorf("expected the list to say %q, got:\n%s", want, listed)
		}
	}
}

func TestCardsKeepNothingOfACardThatWasNotSent(t *testing.T) {
	cards := newCards(apptest.NewFakePostern(), &bytes.Buffer{})
	cards.GovernorKey = ""
	if _, _, err := cards.Send(context.Background(), application.CardSendRequest{Title: "Top 5", Items: []string{"Read it"}}); err == nil {
		t.Fatal("expected a refusal")
	}
	if records, _ := cards.Log.List(context.Background()); len(records) != 0 {
		t.Fatalf("expected nothing kept, got %+v", records)
	}
}

func TestCardsListSaysSoWhenNoneWasSent(t *testing.T) {
	var out bytes.Buffer
	if err := newCards(apptest.NewFakePostern(), &out).List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no card") {
		t.Fatalf("expected a line saying no card was sent, got %q", out.String())
	}
}

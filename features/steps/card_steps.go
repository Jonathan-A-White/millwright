package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

const (
	cardMayorKey    = "card-mayor-key"
	cardGovernorKey = "card-governor-key"
)

// cardContext sends cards to a fake backend with a cipher whose seal it can
// open, so a scenario reads back exactly what the Governor would.
type cardContext struct {
	backend *apptest.FakePostern
	prompts *apptest.FakePrompts
	tracker *apptest.FakeTracker
	log     *apptest.FakeCardLog
	out     strings.Builder
	txid    string
	err     error
}

// InitializeCardScenario registers the steps of features/card.feature.
func InitializeCardScenario(ctx *godog.ScenarioContext) {
	c := &cardContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = cardContext{backend: apptest.NewFakePostern(), prompts: apptest.NewFakePrompts(),
			tracker: apptest.NewFakeTracker(), log: apptest.NewFakeCardLog()}
		c.tracker.AddEpic("mw-epic", domain.Path{})
		for _, id := range []string{"mw-b", "mw-v.1"} {
			c.tracker.AddStory("mw-epic", domain.Story{ID: id, Title: id})
		}
		return ctx, nil
	})

	ctx.Given(`^the backend holds the prompt "([^"]*)" for a card$`, c.theBackendHoldsThePrompt)
	ctx.When(`^the Mayor sends the card "([^"]*)" with the items:$`, c.theMayorSendsTheCard)
	ctx.When(`^the Mayor updates the card "([^"]*)" adding the item "([^"]*)", linking item (\d+) to "([^"]*)" and ticking item (\d+)$`, c.theMayorUpdatesTheCard)
	ctx.When(`^the Mayor runs the prompt "([^"]*)" as the card "([^"]*)" with the item "([^"]*)"$`, c.theMayorRunsThePromptAsACard)

	ctx.Then(`^the list of sent cards has (\d+) entr(?:y|ies)$`, c.theListHasEntries)
	ctx.Then(`^listing the cards prints "([^"]*)" before "([^"]*)"$`, c.listingPrintsBefore)
	ctx.Then(`^the card is delivered as one record of class "([^"]*)" from the Mayor to the Governor, with no summary$`, c.deliveredAsOneRecord)
	ctx.Then(`^the sealed card is titled "([^"]*)"$`, c.theSealedCardIsTitled)
	ctx.Then(`^sealed item (\d+) reads "([^"]*)", links "([^"]*)" and expects "([^"]*)" "([^"]*)"$`, c.sealedItemReads)
	ctx.Then(`^sealed item (\d+) reads "([^"]*)", links "([^"]*)" and expects nothing$`, c.sealedItemExpectsNothing)
	ctx.Then(`^the sealed card subscribes to the kinds "([^"]*)" and the beads "([^"]*)"$`, c.theSealedCardSubscribes)
	ctx.Then(`^the card's txid is printed first$`, c.theTxidIsPrintedFirst)
	ctx.Then(`^the card is refused, saying "([^"]*)"$`, c.theCardIsRefused)
	ctx.Then(`^no card record is delivered$`, c.noCardRecordIsDelivered)
	ctx.Then(`^the sealed update is re "([^"]*)", adds item (\d+) expecting "([^"]*)" "([^"]*)", links item (\d+) to "([^"]*)" and ticks item (\d+)$`, c.theSealedUpdate)
	ctx.Then(`^the sealed card records the prompt "([^"]*)"$`, c.theSealedCardRecordsThePrompt)
}

func (c *cardContext) cards() application.Cards {
	return application.Cards{
		Postern:     c.backend,
		Chain:       apptest.NewFakeChain(c.backend, cardKeys{}),
		Beads:       c.tracker,
		Log:         c.log,
		Cipher:      &apptest.FakeCipher{From: cardMayorKey},
		Keys:        cardKeys{},
		GovernorKey: cardGovernorKey,
		Now:         func() time.Time { return time.Unix(1790000000, 0) },
		Out:         &c.out,
	}
}

func (c *cardContext) theListHasEntries(n int) error {
	records, err := c.log.List(context.Background())
	if err != nil || len(records) != n {
		return fmt.Errorf("expected %d entries in the list of sent cards, got %+v (%v)", n, records, err)
	}
	return nil
}

func (c *cardContext) listingPrintsBefore(first, second string) error {
	var listed strings.Builder
	cards := c.cards()
	cards.Out = &listed
	if err := cards.List(context.Background()); err != nil {
		return err
	}
	a, b := strings.Index(listed.String(), first), strings.Index(listed.String(), second)
	if a < 0 || b < 0 || a > b {
		return fmt.Errorf("expected %q before %q in:\n%s", first, second, listed.String())
	}
	return nil
}

func (c *cardContext) theBackendHoldsThePrompt(name string) error {
	return c.prompts.Put(context.Background(), domain.Prompt{Name: name, Summary: "x", Signature: []string{}, Body: "b"})
}

func (c *cardContext) theMayorSendsTheCard(title string, table *godog.Table) error {
	var items []string
	for _, row := range table.Rows[1:] {
		items = append(items, row.Cells[0].Value)
	}
	_, c.txid, c.err = c.cards().Send(context.Background(), application.CardSendRequest{Title: title, Items: items})
	return nil
}

func (c *cardContext) theMayorUpdatesTheCard(re, item string, linkN int, bead string, tick int) error {
	_, c.txid, c.err = c.cards().Update(context.Background(), application.CardUpdateRequest{
		Re: re, Items: []string{item}, Links: []string{fmt.Sprintf("%d:%s", linkN, bead)}, Tick: []int{tick},
	})
	return nil
}

func (c *cardContext) theMayorRunsThePromptAsACard(name, title, item string) error {
	_, c.txid, c.err = application.PromptCard{Prompts: c.prompts, Cards: c.cards()}.Run(
		context.Background(), name, application.CardSendRequest{Title: title, Items: []string{item}})
	return nil
}

// record is the one record delivered: its clear payload and the plaintext it
// seals.
func (c *cardContext) record() (application.PosternPayload, string, error) {
	var payload application.PosternPayload
	if c.err != nil {
		return payload, "", fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	delivered := c.backend.Delivered()
	if len(delivered) != 1 {
		return payload, "", fmt.Errorf("expected one record delivered, got %d", len(delivered))
	}
	if strings.Contains(string(delivered[0]), `"summary"`) {
		return payload, "", fmt.Errorf("expected no summary, got %s", delivered[0])
	}
	if err := json.Unmarshal(delivered[0], &payload); err != nil {
		return payload, "", err
	}
	plain, _, err := apptest.NewFakeCipher().Decrypt("priv", payload.Ct)
	return payload, plain, err
}

func (c *cardContext) card() (domain.Card, error) {
	var card domain.Card
	_, plain, err := c.record()
	if err != nil {
		return card, err
	}
	if err := json.Unmarshal([]byte(plain), &card); err != nil {
		return card, fmt.Errorf("the plaintext %q is not a card: %w", plain, err)
	}
	return card, nil
}

func (c *cardContext) item(n int) (domain.CardItem, error) {
	card, err := c.card()
	if err != nil {
		return domain.CardItem{}, err
	}
	for _, item := range card.Items {
		if item.N == n {
			return item, nil
		}
	}
	return domain.CardItem{}, fmt.Errorf("the card has no item %d: %+v", n, card.Items)
}

func (c *cardContext) deliveredAsOneRecord(class string) error {
	payload, _, err := c.record()
	if err != nil {
		return err
	}
	if payload.Class != class || payload.From != cardMayorKey || payload.To != cardGovernorKey || payload.Kind != application.PosternMessageKind {
		return fmt.Errorf("expected a %s record from the Mayor to the Governor, got %+v", class, payload)
	}
	return nil
}

func (c *cardContext) theSealedCardIsTitled(title string) error {
	card, err := c.card()
	if err != nil {
		return err
	}
	if card.Title != title {
		return fmt.Errorf("expected the title %q, got %q", title, card.Title)
	}
	return nil
}

func (c *cardContext) sealedItemReads(n int, text, links, bead, state string) error {
	item, err := c.item(n)
	if err != nil {
		return err
	}
	if item.Text != text || strings.Join(item.Links, ",") != links {
		return fmt.Errorf("expected item %d to read %q and link %q, got %+v", n, text, links, item)
	}
	if item.Expect == nil || item.Expect.Bead != bead || item.Expect.State != state {
		return fmt.Errorf("expected item %d to expect %s %s, got %+v", n, bead, state, item.Expect)
	}
	return nil
}

func (c *cardContext) sealedItemExpectsNothing(n int, text, links string) error {
	item, err := c.item(n)
	if err != nil {
		return err
	}
	if item.Text != text || strings.Join(item.Links, ",") != links || item.Expect != nil {
		return fmt.Errorf("expected item %d to read %q, link %q and expect nothing, got %+v (expect %+v)", n, text, links, item, item.Expect)
	}
	return nil
}

func (c *cardContext) theSealedCardSubscribes(kinds, beads string) error {
	card, err := c.card()
	if err != nil {
		return err
	}
	if strings.Join(card.Subscribe.Kinds, ",") != kinds || strings.Join(card.Subscribe.Beads, ",") != beads {
		return fmt.Errorf("expected kinds %q and beads %q, got %+v", kinds, beads, card.Subscribe)
	}
	return nil
}

func (c *cardContext) theTxidIsPrintedFirst() error {
	if c.txid == "" {
		return fmt.Errorf("no txid was reported")
	}
	if first, _, _ := strings.Cut(c.out.String(), "\n"); first != c.txid {
		return fmt.Errorf("expected the first line to be %q, got:\n%s", c.txid, c.out.String())
	}
	return nil
}

func (c *cardContext) theCardIsRefused(want string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected a refusal saying %q, got %v", want, c.err)
	}
	return nil
}

func (c *cardContext) noCardRecordIsDelivered() error {
	if n := len(c.backend.Delivered()); n != 0 {
		return fmt.Errorf("expected nothing delivered, got %d records", n)
	}
	return nil
}

func (c *cardContext) theSealedUpdate(re string, n int, bead, state string, linkN int, link string, tick int) error {
	_, plain, err := c.record()
	if err != nil {
		return err
	}
	var update domain.CardUpdate
	if err := json.Unmarshal([]byte(plain), &update); err != nil {
		return fmt.Errorf("the plaintext %q is not a card update: %w", plain, err)
	}
	switch {
	case update.Re != re:
		return fmt.Errorf("expected re %q, got %q", re, update.Re)
	case len(update.Items) != 1 || update.Items[0].N != n || update.Items[0].Expect == nil ||
		*update.Items[0].Expect != (domain.Expectation{Bead: bead, State: state}):
		return fmt.Errorf("expected item %d expecting %s %s, got %+v", n, bead, state, update.Items)
	case strings.Join(update.Links[linkN], ",") != link:
		return fmt.Errorf("expected item %d linked to %q, got %v", linkN, link, update.Links)
	case len(update.Tick) != 1 || update.Tick[0] != tick:
		return fmt.Errorf("expected item %d ticked, got %v", tick, update.Tick)
	}
	return nil
}

func (c *cardContext) theSealedCardRecordsThePrompt(name string) error {
	card, err := c.card()
	if err != nil {
		return err
	}
	if card.Prompt != name {
		return fmt.Errorf("expected the card to record the prompt %q, got %q", name, card.Prompt)
	}
	return nil
}

// cardKeys is the Mayor's key as a card send asks for it: only its public
// half, since a card goes by the direct channel here.
type cardKeys struct{}

func (cardKeys) Path() string                                           { return "" }
func (cardKeys) Exists() (bool, error)                                  { return true, nil }
func (cardKeys) Generate() error                                        { return nil }
func (cardKeys) PublicKey() (string, string, error)                     { return cardMayorKey, "", nil }
func (cardKeys) PrivateKeyWIF() (string, error)                         { return "priv", nil }
func (cardKeys) Sign([]application.PosternUtxo, []byte) (string, error) { return "", nil }
func (cardKeys) MarkSpent([]application.PosternUtxo) error              { return nil }
func (cardKeys) MarkSent(string) error                                  { return nil }

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// CardSendRequest is a card as mw card send is asked for it: its title, its
// items each as `[<n>. ]<text>|<links csv>|<bead>:<state>`, the bead whose
// channel it goes to (empty for Factory) and the saved prompt it answers
// (empty for none).
type CardSendRequest struct {
	Title       string
	Items       []string
	BeadChannel string
	Prompt      string
}

// CardUpdateRequest is an update as mw card update is asked for it: the
// card's txid, items each numbered (`<n>. <text>|...`), links each
// `<n>:<bead>`, and the numbers of the items ticked off.
type CardUpdateRequest struct {
	Re    string
	Items []string
	Links []string
	Tick  []int
}

// Cards sends the Mayor's live cards to the Governor: a card record, or a
// card-update record naming the card by its txid, each sealed to the
// Governor as a message is and sent by postern_channel, the float cap checked
// first on the chain. A card record carries no summary, so no word of it is
// ever pushed. It writes nothing but the record.
type Cards struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile

	// GovernorKey is who a card is sealed to — config postern_governor_key.
	GovernorKey string
	// FloatSats is the balance cap a send on the chain keeps to — config
	// postern_float_sats.
	FloatSats int64
	// Channel is how a card travels — config postern_channel; empty is
	// direct.
	Channel string

	// Now stamps the record; the zero value reads the real clock.
	Now func() time.Time
	// Out gets the txid, then the card's items. A nil Out prints nothing.
	Out io.Writer
}

// Send sends the card req describes and reports it with its ID, the txid.
func (c Cards) Send(ctx context.Context, req CardSendRequest) (domain.Card, string, error) {
	return c.send(ctx, "mw card send", req)
}

func (c Cards) send(ctx context.Context, command string, req CardSendRequest) (domain.Card, string, error) {
	items, err := parseCardItems(req.Items)
	if err != nil {
		return domain.Card{}, "", fmt.Errorf("%s: %w", command, err)
	}
	card, err := domain.NewCard(req.Title, items)
	if err != nil {
		return card, "", fmt.Errorf("%s: %w", command, err)
	}
	card.Prompt = strings.TrimSpace(req.Prompt)
	if bead := strings.TrimSpace(req.BeadChannel); bead != "" {
		card.Thread = &domain.CardThread{Bead: bead}
	}
	txid, err := c.seal(ctx, command, CardClass, card)
	if err != nil {
		return card, "", err
	}
	card.ID = txid
	if c.Out != nil {
		var b strings.Builder
		b.WriteString(txid + "\n")
		for _, item := range card.Items {
			fmt.Fprintf(&b, "  %d. %s", item.N, oneLine(item.Text))
			if len(item.Links) > 0 {
				b.WriteString("  [" + strings.Join(item.Links, ", ") + "]")
			}
			if item.Expect != nil {
				fmt.Fprintf(&b, "  expects %s %s", item.Expect.Bead, item.Expect.State)
			}
			b.WriteString("\n")
		}
		io.WriteString(c.Out, b.String())
	}
	return card, txid, nil
}

// Update sends the update req describes and reports it with its txid.
func (c Cards) Update(ctx context.Context, req CardUpdateRequest) (domain.CardUpdate, string, error) {
	const command = "mw card update"
	items, err := parseCardItems(req.Items)
	if err != nil {
		return domain.CardUpdate{}, "", fmt.Errorf("%s: %w", command, err)
	}
	update, err := domain.NewCardUpdate(req.Re, items, req.Links, req.Tick)
	if err != nil {
		return update, "", fmt.Errorf("%s: %w", command, err)
	}
	txid, err := c.seal(ctx, command, CardUpdateClass, update)
	if err != nil {
		return update, "", err
	}
	if c.Out != nil {
		fmt.Fprintln(c.Out, txid)
	}
	return update, txid, nil
}

func parseCardItems(specs []string) ([]domain.CardItem, error) {
	items := make([]domain.CardItem, 0, len(specs))
	for _, spec := range specs {
		item, err := domain.ParseCardItem(spec)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// seal encrypts body's JSON to the Governor and sends it as one record of
// class, with no summary, reporting its txid.
func (c Cards) seal(ctx context.Context, command, class string, body any) (string, error) {
	switch {
	case c.Postern == nil || c.Cipher == nil || c.Keys == nil:
		return "", fmt.Errorf("%s: no postern backend, cipher or key file is configured", command)
	case strings.TrimSpace(c.GovernorKey) == "":
		return "", fmt.Errorf("%s: postern_governor_key is not set, so there is nowhere to send to", command)
	}
	channel, err := posternChannel(c.Channel)
	if err != nil {
		return "", fmt.Errorf("%s: %w", command, err)
	}
	plaintext, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("%s: building the record's plaintext: %w", command, err)
	}
	from, address, err := c.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	send := PosternSend{
		Postern: c.Postern, Cipher: c.Cipher, Keys: c.Keys,
		GovernorKey: c.GovernorKey, FloatSats: c.FloatSats, Now: c.Now, Out: c.Out,
	}
	if channel == PosternChannelChain {
		if err := send.underFloat(ctx, command, address); err != nil {
			return "", err
		}
	}
	txid, err := send.sendOne(ctx, channel, class, "", from, address, string(plaintext))
	if err != nil {
		return "", fmt.Errorf("%s: %w", command, err)
	}
	return txid, nil
}

// PromptCard sends the Mayor's answer to a saved prompt as a live card: the
// send mw card send makes, with the prompt's name recorded on the card. The
// prompt must be saved; its options are not asked for, the run before having
// printed the facts the Mayor composed the items from.
type PromptCard struct {
	Prompts Prompts
	Cards   Cards
}

// Run sends req as the answer to the prompt named name.
func (p PromptCard) Run(ctx context.Context, name string, req CardSendRequest) (domain.Card, string, error) {
	prompt, err := getPrompt(ctx, p.Prompts, "run --card", name)
	if err != nil {
		return domain.Card{}, "", err
	}
	req.Prompt = prompt.Name
	return p.Cards.send(ctx, "mw prompt run --card", req)
}

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

// CardBeads is what Cards reads a bead through, to tell an epic: the work
// tracker's ShowBeads.
type CardBeads interface {
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
}

// CardLog is the Mayor's own list of the cards and card updates it has sent,
// kept on this host so a card's txid, printed once at send, can be found again.
// Append adds a record; List reads them oldest first, none and no error when
// nothing was sent.
type CardLog interface {
	Append(ctx context.Context, record domain.CardRecord) error
	List(ctx context.Context) ([]domain.CardRecord, error)
}

// Cards sends the Mayor's live cards to the Governor: a card record, or a
// card-update record naming the card by its txid, each sealed to the
// Governor as a message is and sent by postern_channel, the float cap checked
// first on the chain. A card record carries no summary, so no word of it is
// ever pushed. It refuses an item that can never tick, an epic expected landed
// or verified, and keeps each card and update it sends in Log.
type Cards struct {
	Postern Postern
	// Chain is what a card on the chain channel goes on.
	Chain  Chain
	Cipher Cipher
	Keys   PosternKeyFile
	// Beads reads the beads an item expects landed or verified, to refuse an
	// epic; Log keeps what is sent.
	Beads CardBeads
	Log   CardLog

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
	if err := c.checkExpectations(ctx, card.Items); err != nil {
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
	kept := c.keep(ctx, txid, domain.RecordOfCard(card, txid, c.stamp()))
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
	return card, txid, kept
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
	if err := c.checkExpectations(ctx, update.Items); err != nil {
		return update, "", fmt.Errorf("%s: %w", command, err)
	}
	txid, err := c.seal(ctx, command, CardUpdateClass, update)
	if err != nil {
		return update, "", err
	}
	kept := c.keep(ctx, txid, domain.RecordOfUpdate(update, txid, c.stamp()))
	if c.Out != nil {
		fmt.Fprintln(c.Out, txid)
	}
	return update, txid, kept
}

// checkExpectations refuses an item that expects a bead to be landed or
// verified when the bead is an epic, which is only ever closed, or cannot be
// read: the card would wait on it for ever.
func (c Cards) checkExpectations(ctx context.Context, items []domain.CardItem) error {
	seen := map[string]bool{}
	for _, item := range items {
		e := item.Expect
		if e == nil || (e.State != domain.ExpectLanded && e.State != domain.ExpectVerified) || seen[e.Bead] {
			continue
		}
		seen[e.Bead] = true
		if c.Beads == nil {
			return fmt.Errorf("cannot read %s: no work tracker is configured", e.Bead)
		}
		found, err := c.Beads.ShowBeads(ctx, []string{e.Bead})
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", e.Bead, err)
		}
		if len(found) == 0 {
			return fmt.Errorf("cannot read %s: the tracker does not know it", e.Bead)
		}
		if why := e.NeverTicks(found[0].IsEpic || strings.TrimSpace(found[0].Type) == "epic"); why != "" {
			return fmt.Errorf("item %d: %s", item.N, why)
		}
	}
	return nil
}

func (c Cards) stamp() string {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return now().UTC().Format(time.RFC3339)
}

// keep appends record to the log of what was sent. A card already sent stays
// sent when this fails: the error says so, and names its txid.
func (c Cards) keep(ctx context.Context, txid string, record domain.CardRecord) error {
	if c.Log == nil {
		return nil
	}
	if err := c.Log.Append(ctx, record); err != nil {
		return fmt.Errorf("sent as %s, but it could not be kept in the list of sent cards: %w", txid, err)
	}
	return nil
}

// List prints the cards and updates sent from this host, newest first: the
// txid, when, the title (or the card an update is of) and each item with what
// it expects.
func (c Cards) List(ctx context.Context) error {
	if c.Log == nil {
		return fmt.Errorf("mw card list: no list of sent cards is configured")
	}
	records, err := c.Log.List(ctx)
	if err != nil {
		return fmt.Errorf("mw card list: %w", err)
	}
	if c.Out == nil {
		return nil
	}
	if len(records) == 0 {
		fmt.Fprintln(c.Out, "no card has been sent from this host")
		return nil
	}
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		what := oneLine(r.Title)
		if r.Re != "" {
			what = "update of " + r.Re
		}
		fmt.Fprintf(c.Out, "%s  %s  %s\n", r.Txid, r.At, what)
		for _, item := range r.Items {
			fmt.Fprintf(c.Out, "  %d. %s", item.N, oneLine(item.Text))
			if item.Expect != nil {
				fmt.Fprintf(c.Out, "  expects %s %s", item.Expect.Bead, item.Expect.State)
			}
			fmt.Fprintln(c.Out)
		}
		if len(r.Tick) > 0 {
			ticks := make([]string, len(r.Tick))
			for j, n := range r.Tick {
				ticks[j] = fmt.Sprint(n)
			}
			fmt.Fprintf(c.Out, "  ticks %s\n", strings.Join(ticks, ", "))
		}
	}
	return nil
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
	from, _, err := c.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	send := PosternSend{
		Postern: c.Postern, Chain: c.Chain, Cipher: c.Cipher, Keys: c.Keys,
		GovernorKey: c.GovernorKey, FloatSats: c.FloatSats, Now: c.Now, Out: c.Out,
	}
	if channel == PosternChannelChain {
		if err := send.underFloat(ctx, command); err != nil {
			return "", err
		}
	}
	txid, err := send.sendOne(ctx, channel, class, "", from, string(plaintext))
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

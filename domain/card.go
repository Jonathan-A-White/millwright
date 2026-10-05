package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// The states an item of a live card may expect of a bead. Answered is the
// card machine's (a question on the bead answered); the rest are the bead's.
// Held is a bead whose status is deferred.
const (
	ExpectOpen     = "open"
	ExpectLanded   = "landed"
	ExpectVerified = "verified"
	ExpectClosed   = "closed"
	ExpectAnswered = "answered"
	ExpectHeld     = "held"
)

// ExpectStates are the states an expectation may name, in the order a refusal
// lists them.
var ExpectStates = []string{ExpectOpen, ExpectLanded, ExpectVerified, ExpectClosed, ExpectAnswered, ExpectHeld}

// OptionExpectStates are the states an option of a question may expect of a
// bead: ExpectStates less answered, which is not a state of the bead.
var OptionExpectStates = []string{ExpectOpen, ExpectLanded, ExpectVerified, ExpectClosed, ExpectHeld}

// listStates joins states as a refusal names them: "a, b or c".
func listStates(states []string) string {
	return strings.Join(states[:len(states)-1], ", ") + " or " + states[len(states)-1]
}

// Card is a live card: the Mayor's numbered list for the Governor, each item
// with the beads it links to and what it expects of a bead, and the events
// the card subscribes to so the app can tick an item off when its expected
// event arrives. Its JSON is the plaintext a card record seals.
type Card struct {
	// ID is the card record's txid once sent. It is never in the plaintext,
	// which cannot name its own txid: a reader takes it from the record.
	ID    string `json:"-"`
	Title string `json:"title"`
	// Prompt is the saved prompt the card answers, when mw prompt run --card
	// sent it; empty otherwise.
	Prompt string `json:"prompt,omitempty"`
	// Thread is the bead whose channel the card is posted in; nil for Factory.
	Thread    *CardThread   `json:"thread,omitempty"`
	Items     []CardItem    `json:"items"`
	Subscribe CardSubscribe `json:"subscribe"`
}

// CardThread names the bead whose channel a card is in, as a message's
// thread envelope does.
type CardThread struct {
	Bead string `json:"bead"`
}

// CardItem is one numbered item of a card.
type CardItem struct {
	// N is the item's number, from 1.
	N    int    `json:"n"`
	Text string `json:"text"`
	// Links are the bead ids the item links to: where he can go to do it.
	Links []string `json:"links,omitempty"`
	// Expect is the state of a bead that ticks the item off; nil for an item
	// that is only read.
	Expect *Expectation `json:"expect,omitempty"`
	// Done and DoneAt (Unix seconds) are the app's, once the expected event
	// arrives or an update ticks it; a card is sent with neither.
	Done   bool  `json:"done,omitempty"`
	DoneAt int64 `json:"done_at,omitempty"`
}

// Expectation is what an item waits for: Bead reaching State, one of
// ExpectStates.
type Expectation struct {
	Bead  string `json:"bead"`
	State string `json:"state"`
}

// CardSubscribe is what a card listens to: the event kinds its expectations
// need and every bead its items name. Both are lists, empty rather than
// absent.
type CardSubscribe struct {
	Kinds []string `json:"kinds"`
	Beads []string `json:"beads"`
}

// CardUpdate changes a card already sent, named by its txid: items added or,
// by number, replaced; links added to an item; items ticked off. Its JSON is
// the plaintext a card-update record seals.
type CardUpdate struct {
	Re    string           `json:"re"`
	Items []CardItem       `json:"items,omitempty"`
	Links map[int][]string `json:"links,omitempty"`
	Tick  []int            `json:"tick,omitempty"`
}

// cardBeadPattern is a bead id as an item's text names one: the rig's prefix,
// one hyphen and the hash, then any child numbers (mw-nqur1n.10).
var cardBeadPattern = regexp.MustCompile(`^[a-z][a-z0-9]*-[a-z0-9]+(\.[0-9]+)*$`)

// cardItemNumber is the "<n>. " an item's text may begin with.
var cardItemNumber = regexp.MustCompile(`^(\d+)\.\s+`)

// cardAsks are the asks an expectation is derived from, each the start of an
// item's text, and the state the bead it names is expected to reach.
var cardAsks = []struct{ ask, state string }{
	{"verified on ", ExpectVerified},
	{"looks good on ", ExpectClosed},
	{"approve ", ExpectAnswered},
	{"answer ", ExpectAnswered},
	{"release ", ExpectOpen},
}

// ValidExpectState reports whether state is one an expectation may name.
func ValidExpectState(state string) bool {
	for _, s := range ExpectStates {
		if s == state {
			return true
		}
	}
	return false
}

func validBeadID(id string) bool {
	return id != "" && !strings.ContainsAny(id, " \t\n|,")
}

// DeriveExpectation is the expectation an item's ask implies, read from the
// start of text, in any case: VERIFIED on X expects X verified, Looks good on
// X expects X closed, Approve X and Answer X expect X answered, and Release X
// expects X open. X is the first bead id in the rest of the text that is
// among links, else the first link, else the first bead id in the text; nil
// when the text makes no ask or names no bead.
func DeriveExpectation(text string, links []string) *Expectation {
	lower := strings.ToLower(strings.TrimSpace(text))
	rest, state := "", ""
	for _, a := range cardAsks {
		if strings.HasPrefix(lower, a.ask) {
			rest, state = strings.TrimSpace(text)[len(a.ask):], a.state
			break
		}
	}
	if state == "" {
		return nil
	}
	var named []string
	for _, word := range strings.Fields(rest) {
		if word = strings.Trim(word, ",:;.!?()[]\"'"); cardBeadPattern.MatchString(word) {
			named = append(named, word)
		}
	}
	bead := ""
	switch {
	case len(links) > 0:
		bead = links[0]
		for _, id := range named {
			if containsString(links, id) {
				bead = id
				break
			}
		}
	case len(named) > 0:
		bead = named[0]
	default:
		return nil
	}
	return &Expectation{Bead: bead, State: state}
}

// ParseExpectation reads "<bead>:<state>".
func ParseExpectation(spec string) (*Expectation, error) {
	bead, state, ok := strings.Cut(strings.TrimSpace(spec), ":")
	bead, state = strings.TrimSpace(bead), strings.TrimSpace(state)
	if !ok || bead == "" || state == "" {
		return nil, fmt.Errorf("%q is not an expectation: give <bead>:<state>", spec)
	}
	expect := &Expectation{Bead: bead, State: state}
	return expect, expect.validate()
}

func (e Expectation) validate() error {
	if !validBeadID(e.Bead) {
		return fmt.Errorf("%q is not a bead id", e.Bead)
	}
	if !ValidExpectState(e.State) {
		return fmt.Errorf("%q is not a state an item may expect: %s", e.State, listStates(ExpectStates))
	}
	return nil
}

// ParseCardItem reads an item as the Mayor gives it,
// `[<n>. ]<text>|<links csv>|<bead>:<state>`: the links and the expectation
// may be left empty or out, and an item that gives no expectation has one
// derived from its ask (DeriveExpectation). N is 0 when the text does not
// begin with its number.
func ParseCardItem(spec string) (CardItem, error) {
	var item CardItem
	parts := strings.Split(spec, "|")
	if len(parts) > 3 {
		return item, fmt.Errorf("%q is not an item: give <text>|<links>|<bead>:<state>, the text holding no |", spec)
	}
	text := strings.TrimSpace(parts[0])
	if m := cardItemNumber.FindStringSubmatch(text); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return item, fmt.Errorf("%q: an item is numbered from 1", spec)
		}
		item.N, text = n, strings.TrimSpace(text[len(m[0]):])
	}
	if text == "" {
		return item, fmt.Errorf("%q: the item has no text", spec)
	}
	item.Text = text
	if len(parts) > 1 {
		for _, id := range strings.Split(parts[1], ",") {
			if id = strings.TrimSpace(id); id == "" {
				continue
			}
			if !validBeadID(id) {
				return item, fmt.Errorf("%q: the link %q is not a bead id", spec, id)
			}
			item.Links = append(item.Links, id)
		}
	}
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		expect, err := ParseExpectation(parts[2])
		if err != nil {
			return item, fmt.Errorf("item %q: %w", text, err)
		}
		item.Expect = expect
		return item, nil
	}
	item.Expect = DeriveExpectation(text, item.Links)
	return item, nil
}

// NewCard is the card titled title with items: an item not numbered takes
// its place in the list as its number, and the card subscribes to what its
// items need (Subscriptions). A card with no title or no items, an item
// numbered twice or an expectation that is not one is refused.
func NewCard(title string, items []CardItem) (Card, error) {
	card := Card{Title: strings.TrimSpace(title)}
	if card.Title == "" {
		return card, fmt.Errorf("the card has no title: give --title")
	}
	if len(items) == 0 {
		return card, fmt.Errorf("the card has no items: give one --item or more")
	}
	for i, item := range items {
		if item.N == 0 {
			item.N = i + 1
		}
		card.Items = append(card.Items, item)
	}
	if err := checkCardItems(card.Items); err != nil {
		return card, err
	}
	card.Subscribe = Subscriptions(card.Items)
	return card, nil
}

// Subscriptions is what items need to hear: the event kind each expectation
// waits for (bead_changed for a bead's state, card_answered for answered) and
// every bead an item expects of or links to, each once, in the order the
// items first name them.
func Subscriptions(items []CardItem) CardSubscribe {
	sub := CardSubscribe{Kinds: []string{}, Beads: []string{}}
	addBead := func(id string) {
		if !containsString(sub.Beads, id) {
			sub.Beads = append(sub.Beads, id)
		}
	}
	for _, item := range items {
		if item.Expect != nil {
			kind := events.KindBeadChanged
			if item.Expect.State == ExpectAnswered {
				kind = events.KindCardAnswered
			}
			if !containsString(sub.Kinds, kind) {
				sub.Kinds = append(sub.Kinds, kind)
			}
			addBead(item.Expect.Bead)
		}
		for _, id := range item.Links {
			addBead(id)
		}
	}
	return sub
}

// NewCardUpdate is the update of the card whose txid is re: items, each
// numbered, added or replaced by number; links, each "<n>:<bead>", added to
// item n; and tick, the numbers of the items ticked off. An update that names
// no card or changes nothing is refused.
func NewCardUpdate(re string, items []CardItem, links []string, tick []int) (CardUpdate, error) {
	update := CardUpdate{Re: strings.TrimSpace(re)}
	if update.Re == "" {
		return update, fmt.Errorf("which card? give the txid mw card send printed")
	}
	if len(items) == 0 && len(links) == 0 && len(tick) == 0 {
		return update, fmt.Errorf("nothing to change: give --item, --link or --tick")
	}
	for _, item := range items {
		if item.N == 0 {
			return update, fmt.Errorf("item %q: an update says which item it adds or replaces: '<n>. <text>|...'", item.Text)
		}
	}
	if err := checkCardItems(items); err != nil {
		return update, err
	}
	update.Items = items
	for _, spec := range links {
		number, id, ok := strings.Cut(spec, ":")
		n, err := strconv.Atoi(strings.TrimSpace(number))
		id = strings.TrimSpace(id)
		if !ok || err != nil || id == "" {
			return update, fmt.Errorf("%q is not a link: give <n>:<bead>", spec)
		}
		if n < 1 {
			return update, fmt.Errorf("%q: an item is numbered from 1", spec)
		}
		if !validBeadID(id) {
			return update, fmt.Errorf("%q: %q is not a bead id", spec, id)
		}
		if update.Links == nil {
			update.Links = map[int][]string{}
		}
		update.Links[n] = append(update.Links[n], id)
	}
	for _, n := range tick {
		if n < 1 {
			return update, fmt.Errorf("--tick %d: an item is numbered from 1", n)
		}
	}
	update.Tick = append([]int(nil), tick...)
	sort.Ints(update.Tick)
	return update, nil
}

// checkCardItems refuses a number given twice, and an expectation that is
// not one.
func checkCardItems(items []CardItem) error {
	seen := map[int]bool{}
	for _, item := range items {
		if seen[item.N] {
			return fmt.Errorf("item %d twice: each item has its own number", item.N)
		}
		seen[item.N] = true
		if item.Expect != nil {
			if err := item.Expect.validate(); err != nil {
				return fmt.Errorf("item %d: %w", item.N, err)
			}
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// SplitOption reads an option as mw postern send --option gives it,
// `<text>[|<bead>:<state>[,<bead>:<state>...]]`: the text the card shows, and
// what the option expects of beads for the Governor's own acts to have
// answered it. An option with no '|' expects nothing. A bead that is not
// one, or a state that is not among OptionExpectStates, is refused.
func SplitOption(option string) (text string, expect []Expectation, err error) {
	text, specs, found := strings.Cut(option, "|")
	text = strings.TrimSpace(text)
	if !found {
		return text, nil, nil
	}
	if text == "" {
		return "", nil, fmt.Errorf("%q: the option has no text before the |", option)
	}
	for _, spec := range strings.Split(specs, ",") {
		bead, state, ok := strings.Cut(strings.TrimSpace(spec), ":")
		bead, state = strings.TrimSpace(bead), strings.TrimSpace(state)
		switch {
		case !ok || bead == "" || state == "":
			return "", nil, fmt.Errorf("option %q: %q is not an expectation: give <bead>:<state>", text, spec)
		case !validBeadID(bead):
			return "", nil, fmt.Errorf("option %q: %q is not a bead id", text, bead)
		case !containsString(OptionExpectStates, state):
			return "", nil, fmt.Errorf("option %q: %q is not a state an option may expect: %s", text, state, listStates(OptionExpectStates))
		}
		expect = append(expect, Expectation{Bead: bead, State: state})
	}
	return text, expect, nil
}

// CardRecord is one line of the Mayor's own list of the cards and card
// updates it has sent: what mw card list prints, so a card can be found, and
// its txid used, after the one print of it is gone. An update names its card
// in Re and has no title.
type CardRecord struct {
	Txid  string           `json:"txid"`
	Re    string           `json:"re,omitempty"`
	Title string           `json:"title,omitempty"`
	At    string           `json:"at"`
	Items []CardRecordItem `json:"items"`
	Tick  []int            `json:"tick,omitempty"`
}

// CardRecordItem is an item as CardRecord keeps it: its number, text and what
// it expects.
type CardRecordItem struct {
	N      int          `json:"n"`
	Text   string       `json:"text"`
	Expect *Expectation `json:"expect,omitempty"`
}

// RecordOfCard is the record of card as sent under txid at at.
func RecordOfCard(card Card, txid, at string) CardRecord {
	return CardRecord{Txid: txid, Title: card.Title, At: at, Items: recordItems(card.Items)}
}

// RecordOfUpdate is the record of update as sent under txid at at.
func RecordOfUpdate(update CardUpdate, txid, at string) CardRecord {
	return CardRecord{Txid: txid, Re: update.Re, At: at, Items: recordItems(update.Items), Tick: update.Tick}
}

func recordItems(items []CardItem) []CardRecordItem {
	kept := make([]CardRecordItem, 0, len(items))
	for _, item := range items {
		kept = append(kept, CardRecordItem{N: item.N, Text: item.Text, Expect: item.Expect})
	}
	return kept
}

// NeverTicks says why e can never be met, or "" when it can: an epic never
// reaches landed or verified, only closed.
func (e Expectation) NeverTicks(isEpic bool) string {
	if isEpic && (e.State == ExpectLanded || e.State == ExpectVerified) {
		return fmt.Sprintf("%s is an epic: an epic is never landed or verified; expect closed", e.Bead)
	}
	return ""
}

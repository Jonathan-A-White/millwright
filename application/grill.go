package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// GrillKey is the note key a grilling ticket keeps its running card under: the
// card's txid and one entry per question asked (grillNote).
func GrillKey(bead string) string { return "postern.grill." + bead }

// grillQuestionLimit is the most runes of a question one item of the running
// card shows.
const grillQuestionLimit = 300

// grillNote is what a ticket's GrillKey note holds: the running card, once
// sent, and each question asked, in the order asked. Item n of the card is
// Items[n-1].
type grillNote struct {
	// Card is the running card's txid; empty until a card has been sent.
	Card  string      `json:"card,omitempty"`
	Items []grillItem `json:"items"`
}

// grillItem is one question of a grilling: the decision card it was asked
// under, and, once the Governor has answered, the answer and when.
type grillItem struct {
	Q      string `json:"q"`
	Txid   string `json:"txid"`
	Answer string `json:"answer,omitempty"`
	At     string `json:"at,omitempty"`
}

// spec is item n of the running card as mw card update and mw card send are
// given it: open, the question and the decision card it waits on; answered,
// the answer and when. It links the ticket, whose channel holds the decision
// card.
func (g grillItem) spec(n int, bead string) string {
	q := clippedTo(oneLine(g.Q), grillQuestionLimit)
	state := fmt.Sprintf("open, waiting on card %s", shortTxid(g.Txid))
	if g.Answer != "" {
		state = fmt.Sprintf("answered %s at %s (card %s)", oneLine(g.Answer), g.At, shortTxid(g.Txid))
	}
	return fmt.Sprintf("%d. Q%d: %s — %s|%s|", n, n, q, state, bead)
}

func (n grillNote) specs(bead string) []string {
	specs := make([]string, len(n.Items))
	for i, item := range n.Items {
		specs[i] = item.spec(i+1, bead)
	}
	return specs
}

func readGrillNote(ctx context.Context, notes PosternNotes, bead string) (grillNote, error) {
	value, err := notes.Note(ctx, GrillKey(bead))
	if err != nil {
		return grillNote{}, err
	}
	var note grillNote
	if strings.TrimSpace(value) == "" {
		return note, nil
	}
	if err := json.Unmarshal([]byte(value), &note); err != nil {
		return grillNote{}, fmt.Errorf("%s's grilling note %q does not read: %w", bead, value, err)
	}
	return note, nil
}

func writeGrillNote(ctx context.Context, notes PosternNotes, bead string, note grillNote) error {
	value, err := json.Marshal(note)
	if err != nil {
		return err
	}
	return notes.SetNote(ctx, GrillKey(bead), string(value))
}

// GrillAsk is one question of a grilling, as mw grill is asked for it: the
// ticket, the question, the option it recommends and every option offered.
type GrillAsk struct {
	Bead      string
	Question  string
	Recommend string
	Options   []string
}

// GrillAsked is what Ask sent: the decision card's txid and the running
// card's.
type GrillAsked struct {
	Question string
	Card     string
}

// Grill asks a grilling ticket's questions: each as a decision card, and each
// on the ONE running card kept on the ticket's channel — an item per question,
// created with the first and updated with each new question; an answer applied
// by mw postern inbox marks its item answered (PosternInbox.grillAnswered).
type Grill struct {
	// Send sends the decision card and records its question on the ticket.
	Send PosternSend
	// Cards sends and updates the running card.
	Cards Cards
	// Tracker reads the ticket, to tell it is a grilling.
	Tracker CardBeads
	// Notes keeps the running card's txid and its items under GrillKey.
	Notes PosternNotes
}

// Ask asks req on its ticket and keeps the running card current. The ticket
// must carry the grilling label. A decision card already sent stays sent when
// the running card cannot be: the error says so, names its txid, and the
// question is kept, so the next question sends the card with it.
func (g Grill) Ask(ctx context.Context, req GrillAsk) (GrillAsked, error) {
	const command = "mw grill"
	bead := strings.TrimSpace(req.Bead)
	switch {
	case bead == "":
		return GrillAsked{}, fmt.Errorf("%s: which ticket? give its bead id", command)
	case strings.TrimSpace(req.Question) == "":
		return GrillAsked{}, fmt.Errorf("%s: what is the question? give --ask", command)
	case g.Tracker == nil || g.Notes == nil:
		return GrillAsked{}, fmt.Errorf("%s: no work tracker or notes are configured", command)
	}
	found, err := g.Tracker.ShowBeads(ctx, []string{bead})
	if err != nil {
		return GrillAsked{}, fmt.Errorf("%s: reading %s: %w", command, bead, err)
	}
	if len(found) == 0 {
		return GrillAsked{}, fmt.Errorf("%s: the tracker does not know %s", command, bead)
	}
	if !containsLabel(found[0].Labels, "grilling") {
		return GrillAsked{}, fmt.Errorf("%s: %s is not a grilling ticket (it has no label grilling): send other questions with mw postern send --class decision-needed", command, bead)
	}
	note, err := readGrillNote(ctx, g.Notes, bead)
	if err != nil {
		return GrillAsked{}, fmt.Errorf("%s: %w", command, err)
	}

	txid, err := g.Send.Run(ctx, PosternSendRequest{
		Class: "decision-needed", Text: req.Question, Bead: bead, Recommend: req.Recommend, Options: req.Options,
	})
	if err != nil {
		return GrillAsked{}, err
	}
	asked := GrillAsked{Question: txid}
	note.Items = append(note.Items, grillItem{Q: req.Question, Txid: txid})
	n := len(note.Items)

	var cardErr error
	if note.Card == "" {
		title := fmt.Sprintf("Grilling %s: %s", bead, oneLine(found[0].Story.Title))
		_, note.Card, cardErr = g.Cards.Send(ctx, CardSendRequest{Title: title, Items: note.specs(bead), BeadChannel: bead})
	} else {
		_, _, cardErr = g.Cards.Update(ctx, CardUpdateRequest{Re: note.Card, Items: []string{note.Items[n-1].spec(n, bead)}})
	}
	asked.Card = note.Card
	if err := writeGrillNote(ctx, g.Notes, bead, note); err != nil {
		return asked, fmt.Errorf("%s: asked as %s, but %s's grilling note could not be kept: %w", command, txid, bead, err)
	}
	if cardErr != nil {
		return asked, fmt.Errorf("%s: asked as %s, but the running card could not be kept current: %w", command, txid, cardErr)
	}
	return asked, nil
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

// grillAnswered marks the item of reply's bead's running card that reply
// answers as answered, with the answer and the time m was sent, and ticks it
// off. The item is the question reply.Re names, else the one the bead's open
// question note (asked, read before it was cleared) was asked under,
// else the newest still open. A bead with no running card, no Cards to update
// it, or an item answered already changes nothing. A card that cannot be
// updated is said, never returned: the answer is already recorded.
func (i PosternInbox) grillAnswered(ctx context.Context, m PosternInboxMessage, reply PosternReply, asked posternQuestionNote) {
	if i.Cards == nil || i.Memory == nil {
		return
	}
	note, err := readGrillNote(ctx, i.Memory, reply.Bead)
	if err != nil {
		i.printf("mw postern inbox: the answer to %s is recorded, but its grilling card was not updated: %v\n", reply.Bead, err)
		return
	}
	if note.Card == "" || len(note.Items) == 0 {
		return
	}
	n := 0
	re := strings.TrimSpace(reply.Re)
	for _, txid := range []string{re, asked.Txid} {
		for j, item := range note.Items {
			if txid != "" && item.Txid == txid {
				n = j + 1
			}
		}
		if n != 0 || txid == re && re != "" {
			// A tap naming a card that is not a question of this grilling
			// (a button left from another card) answers none of them.
			break
		}
	}
	if n == 0 && re == "" {
		for j := len(note.Items) - 1; j >= 0 && n == 0; j-- {
			if note.Items[j].Answer == "" {
				n = j + 1
			}
		}
	}
	if n == 0 || note.Items[n-1].Answer != "" {
		return
	}
	at := sentInFull(m.Ts)
	if at == "" {
		at = i.Cards.stamp()
	}
	note.Items[n-1].Answer, note.Items[n-1].At = reply.Answer, at
	if _, _, err := i.Cards.Update(ctx, CardUpdateRequest{
		Re: note.Card, Items: []string{note.Items[n-1].spec(n, reply.Bead)}, Tick: []int{n},
	}); err != nil {
		i.printf("mw postern inbox: the answer to %s is recorded, but its grilling card was not updated: %v\n", reply.Bead, err)
		return
	}
	if err := writeGrillNote(ctx, i.Memory, reply.Bead, note); err != nil {
		i.printf("mw postern inbox: the grilling card of %s was updated, but its note could not be kept: %v\n", reply.Bead, err)
	}
}

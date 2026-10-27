package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PosternClearComment begins the comment mw postern clear leaves on the bead
// whose card it cleared; the why follows it.
const PosternClearComment = "Card cleared by mw postern clear: "

// PosternClearReply begins the reply mw postern clear posts under the card;
// the why follows it.
const PosternClearReply = "This card is cleared: "

// PosternClear clears a Postern card the Governor has answered while its bead
// stays open, which mw tidy does only for a closed bead (row 3 of
// docs/tidy.md). It does three things: clears the bead's question note
// (PosternQuestionKey), comments the bead with the why, and posts a short reply
// under the card's txid so his thread closes the loop. The bead itself is never
// changed.
type PosternClear struct {
	Beads  TidyBeads
	Notes  PosternNotes
	Sender PosternSender
	// Txid, when set, withdraws only the card asked under that txid: the
	// newest card of the bead's question note, or one of its Earlier. The
	// other cards and their note stay. Unset, the whole note is cleared and the
	// reply goes under the newest card.
	Txid string
	// DryRun prints the three acts and does none of them.
	DryRun bool
	// Out is where each act is said. A nil Out says nothing.
	Out io.Writer
}

// Run clears bead's card for why. A bead with no question note is refused,
// naming it, and a blank why is refused. The note is cleared and the bead
// commented before the reply is sent, so a reply that cannot be sent leaves the
// card cleared and says so as an error. A note that never recorded a txid has no
// thread to reply under, so no reply is sent for it.
func (c PosternClear) Run(ctx context.Context, bead, why string) error {
	why = strings.Join(strings.Fields(why), " ")
	if why == "" {
		return fmt.Errorf("mw postern clear: say why with --why \"<text>\": it goes on %s and under the card", bead)
	}
	if c.Beads == nil || c.Notes == nil {
		return fmt.Errorf("mw postern clear: no work tracker is configured")
	}
	value, err := c.Notes.Note(ctx, PosternQuestionKey(bead))
	if err != nil {
		return fmt.Errorf("mw postern clear: reading %s's question note: %w", bead, err)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("mw postern clear: %s has no question note, so there is no card to clear", bead)
	}
	card, _ := decisionCardOf(value)
	txid := strings.TrimSpace(card.Txid)
	comment := PosternClearComment + why
	reply := PosternClearReply + why

	// remaining is the note left after a --txid card goes: empty when the
	// whole note is cleared.
	remaining := ""
	if want := strings.TrimSpace(c.Txid); want != "" {
		var err error
		if remaining, err = withdrawCard(card, want); err != nil {
			return fmt.Errorf("mw postern clear: %s: %w", bead, err)
		}
		txid = want
		comment += " (card " + want + ")"
	}

	if c.DryRun {
		if remaining != "" {
			c.printf("would withdraw card %s from the question note of %s and keep the others\n", txid, bead)
		} else {
			c.printf("would clear the question note of %s\n", bead)
		}
		c.printf("would comment on %s: %s\n", bead, comment)
		if txid == "" {
			c.printf("would send no reply: the note records no txid\n")
		} else {
			c.printf("would reply under %s: %s\n", txid, reply)
		}
		return nil
	}
	if remaining != "" {
		if err := c.Notes.SetNote(ctx, PosternQuestionKey(bead), remaining); err != nil {
			return fmt.Errorf("mw postern clear: withdrawing card %s from %s's question note: %w", txid, bead, err)
		}
		c.printf("withdrew card %s from the question note of %s\n", txid, bead)
	} else {
		if err := c.Notes.ClearNote(ctx, PosternQuestionKey(bead)); err != nil {
			return fmt.Errorf("mw postern clear: clearing %s's question note: %w", bead, err)
		}
		c.printf("cleared the question note of %s\n", bead)
	}
	if err := c.Beads.CommentOnStory(ctx, bead, comment); err != nil {
		return fmt.Errorf("mw postern clear: %s's note is cleared, but commenting on it failed: %w", bead, err)
	}
	c.printf("commented on %s\n", bead)
	if txid == "" {
		c.printf("sent no reply: the note records no txid\n")
		return nil
	}
	if c.Sender == nil {
		return fmt.Errorf("mw postern clear: %s's card is cleared, but no postern sender is configured to tell him under it", bead)
	}
	if _, err := c.Sender.Run(ctx, PosternSendRequest{Class: "message", Text: reply, Thread: bead, Re: txid, Recorded: true}); err != nil {
		return fmt.Errorf("mw postern clear: %s's card is cleared, but telling him under it failed: %w", bead, err)
	}
	c.printf("replied under %s\n", txid)
	return nil
}

func (c PosternClear) printf(format string, args ...any) {
	if c.Out != nil {
		fmt.Fprintf(c.Out, format, args...)
	}
}

// withdrawCard is the question note left once the card asked under txid goes,
// as JSON, or "" when no card is left and the note is to be cleared. When the
// newest card goes, the newest card it overtook leads the note again. A txid
// no card of note holds is an error naming it.
func withdrawCard(note posternQuestionNote, txid string) (string, error) {
	cards := append(append([]posternQuestionNote{}, note.Earlier...), note.withoutEarlier())
	kept := make([]posternQuestionNote, 0, len(cards))
	found := false
	for _, card := range cards {
		if strings.TrimSpace(card.Txid) == txid {
			found = true
			continue
		}
		kept = append(kept, card)
	}
	if !found {
		return "", fmt.Errorf("no card with txid %s is on its question note", txid)
	}
	if len(kept) == 0 {
		return "", nil
	}
	newest := kept[len(kept)-1]
	newest.Earlier = kept[:len(kept)-1]
	if len(newest.Earlier) == 0 {
		newest.Earlier = nil
	}
	encoded, err := json.Marshal(newest)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

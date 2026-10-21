package application

import (
	"context"
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

	if c.DryRun {
		c.printf("would clear the question note of %s\n", bead)
		c.printf("would comment on %s: %s\n", bead, comment)
		if txid == "" {
			c.printf("would send no reply: the note records no txid\n")
		} else {
			c.printf("would reply under %s: %s\n", txid, reply)
		}
		return nil
	}
	if err := c.Notes.ClearNote(ctx, PosternQuestionKey(bead)); err != nil {
		return fmt.Errorf("mw postern clear: clearing %s's question note: %w", bead, err)
	}
	c.printf("cleared the question note of %s\n", bead)
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

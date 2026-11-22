package application

import (
	"context"
	"fmt"
	"strings"
)

// MailHandedBack is the verdict a close-out mails when a Builder handed its
// story back in its closing comment and left the formula's steps open
// (mw-gq6.1651): the story is held, not re-opened, and the Mayor reads why.
const MailHandedBack = "Handed back"

// handedBackWords is what the first line of a closing comment begins with when
// the Builder hands the story back, in any letter case.
const handedBackWords = "handed back"

// isHandBackComment says a comment is a Builder's hand-back: it holds the line
// 'For the rig memory' and its first line begins 'Handed back'.
func isHandBackComment(text string) bool {
	if !strings.Contains(text, "For the rig memory") {
		return false
	}
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(first)), handedBackWords)
}

// handedBackOnStop says whether a stop for open steps is a Builder's hand-back:
// the newest comment written after the branch's newest commit that holds the
// rig memory line begins 'Handed back'. A story so handed back has nothing for
// another attempt to do that the Builder could not, so stop holds it and mails
// it as handed back. A read that fails is a note and the stop stands as a
// refusal, as before.
func (n Next) handedBackOnStop(ctx context.Context, c *closeOut, report *NextReport, reason Reason) bool {
	if reason != ReasonOpenSteps || n.Landing == nil || n.Tracker == nil {
		return false
	}
	newest, comments, err := n.newestCommitAndComments(ctx, c, report, StartPoint(n.remote(), c.target))
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %v, so whether its closing comment hands it back was not known", c.id, err))
		return false
	}
	handedBack := false
	for _, comment := range comments {
		if comment.Created.After(newest) && strings.Contains(comment.Text, "For the rig memory") {
			handedBack = isHandBackComment(comment.Text)
		}
	}
	return handedBack
}

// handedBackHeld is the HELD: comment of a story held for a hand-back.
func handedBackHeld(id string) string {
	return fmt.Sprintf("%s the closing comment of %s hands it back ('Handed back'), so it is held with that reason and not re-opened: "+
		"a new attempt would meet the dead end the Builder met. The Mayor reads the comment and re-scopes or closes the story; "+
		"to land it as it stands, %s.", HeldCommentPrefix, id, OpenStepsWayOut(id))
}

// heldCommentOnStop is the HELD: comment of a story stop holds: for a hand-back,
// or for a stub closing comment, why it is stopped.
func heldCommentOnStop(handedBack bool, id, why string) string {
	if handedBack {
		return handedBackHeld(id)
	}
	return HeldCommentPrefix + " " + why + ". " + StubHeldSaid
}

// handedBackVerdict is the verdict of the mail of a stop: Handed back for a
// hand-back, the verdict the stop had otherwise.
func handedBackVerdict(handedBack bool, verdict string) string {
	if handedBack {
		return MailHandedBack
	}
	return verdict
}

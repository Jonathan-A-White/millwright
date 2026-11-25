package application

import (
	"context"
	"fmt"
)

// MailUnreadablePath is the subject of the mail to the Mayor that a story queued
// for batch landing was handed back because its path cannot be read.
const MailUnreadablePath = "Path unreadable"

// handBackUnreadablePath answers a story queued for batch landing whose path
// cannot be read (perr): no batch can say what rig or branch it lands on, so
// every batch left it out with a note nobody read, and it stayed queued for ever
// (mw-gq6.1734). It is handed back as a story the batches set aside again and
// again is: its run state is cleared and its claim given back, a comment says
// why, and the Mayor is mailed its name, for the path is the Mayor's to mend.
//
// A story whose own path names another rig is that rig's batch's to answer; one
// that names none is answered by whichever batch reads it first. A dry run says
// what it would do and changes nothing. A story that cannot be handed back stays
// queued, and the report says why.
func (b BatchLand) handBackUnreadablePath(ctx context.Context, rig string, detail StoryDetail, perr error, report *BatchReport) {
	id := detail.Story.ID
	if other := detail.Merged().Rig; other != "" && other != rig {
		return
	}
	n := b.Next
	left := fmt.Sprintf("%s is queued, but its path cannot be read, so it is left out: %v", id, perr)
	if b.DryRun || n.Tracker == nil {
		report.Notes = append(report.Notes, left)
		return
	}
	s := BatchStory{ID: id, Title: detail.Story.Title, QueuedAt: detail.QueuedAt, detail: detail}
	why, gone := b.giveBack(ctx, s, report)
	if gone {
		return
	}
	if why != "" {
		report.Notes = append(report.Notes, fmt.Sprintf("%s; it could not be handed back, so it stays queued: %s", left, why))
		return
	}
	comment := fmt.Sprintf("Handed back by a batch of %s: this story's path cannot be read (%v)\n"+
		"A batch cannot tell what rig or branch it lands on, so it was left out of every batch and stayed queued. "+
		"Its run state %s is cleared and its claim given back. Mend the path (mw path set <story> ...), and the story is worked and queued again.",
		rig, perr, RunState+":"+RunQueued)
	if err := writeRetrying(ctx, n, nil, "the hand-back note", func() error { return n.Tracker.CommentOnStory(ctx, id, comment) }); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s was handed back, but the comment saying why could not be written on it: %v", id, err))
	}
	report.Notes = append(report.Notes, fmt.Sprintf("%s was handed back: its path cannot be read, so no batch could take it: %v", id, perr))
	if n.Mailbox == nil {
		return
	}
	mail := NewMessage{
		From:    SeatIdentity(MwSeat, n.Host),
		To:      MayorMailbox,
		Subject: MailUnreadablePath + ": " + id,
		Body: fmt.Sprintf("mw land on %s, making a batch of %s, found %s (%s) queued with a path that cannot be read: %v\n"+
			"It was handed back (its run state cleared, its claim given back) and a comment says why. The path is to be mended.\n",
			n.Host, rig, id, detail.Story.Title, perr),
	}
	if err := writeRetrying(ctx, n, nil, "the mail to the Mayor", func() error {
		_, err := n.Mailbox.Send(ctx, mail)
		return err
	}); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the mail to %s about %s could not be sent: %v", MayorMailbox, id, err))
	}
}

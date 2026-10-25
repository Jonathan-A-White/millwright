package application

import (
	"context"
	"fmt"
	"strings"
)

// DamperNeedsFilesKey is the principle of the DAMPERS line for a ready story
// with no files key on a rig that expects one: dispatch passes it over at the
// needs-files-key gate from the moment it is open, and the line says so then,
// not after StarvedAfter (mw-gq6.795).
const DamperNeedsFilesKey = "needs files key"

// ReasonNeedsFilesKey is the code the comment on a story carries when mw
// dispatch would not claim it for want of a files key: the marker that a second
// dispatch finding the comment already there says and mails nothing again.
const ReasonNeedsFilesKey = "needs-files-key"

// FilesKeyCommand is the command that gives a story the files key it lacks.
func FilesKeyCommand(id string) string {
	return fmt.Sprintf("mw path set %s files=<csv>", id)
}

// needsFilesKeyAlarm is the DAMPERS line for the ready stories among missing,
// and false when none of them is. A held story is not dispatched, so it is not
// named until it is open.
func needsFilesKeyAlarm(ready []StoryDetail, missing []NoFilesKey) (DamperAlarm, bool) {
	isReady := map[string]bool{}
	for _, d := range ready {
		isReady[d.Story.ID] = true
	}
	var ids []string
	for _, n := range missing {
		if isReady[n.ID] {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) == 0 {
		return DamperAlarm{}, false
	}
	return DamperAlarm{
		Principle: DamperNeedsFilesKey,
		Text:      "dispatch passes these over until each has one (" + FilesKeyCommand("<id>") + ")",
		Beads:     ids,
	}, true
}

// refuseUnkeyed tells the story, and the Mayor, once, that a story was not
// claimed for want of a files key, with the exact command that ends it, so that
// a story the Mayor un-holds or releases without one is not left to starve for
// StarvedAfter before anyone knows (mw-gq6.795). The comment is written first
// and is the once, as for refuseSweepless.
func (d Dispatch) refuseUnkeyed(ctx context.Context, detail StoryDetail, report *DispatchReport) {
	id := detail.Story.ID
	comments, err := d.Tracker.StoryComments(ctx, id)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: whether it was already told it needs a files key could not be read, so nobody is told again: %v", id, err))
		return
	}
	for _, comment := range comments {
		if strings.Contains(comment.Text, ReasonNeedsFilesKey) {
			return
		}
	}

	fix := fmt.Sprintf("Run `%s` with the files it is expected to change.", FilesKeyCommand(id))
	said := fmt.Sprintf("mw dispatch on %s did not claim this story (%s): its rig expects a files key and its path has none. %s It is left open and is dispatched once it has one.",
		d.Host, ReasonNeedsFilesKey, fix)
	if err := d.Tracker.CommentOnStory(ctx, id, said); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the comment about its missing files key could not be written, so nobody is told: %v", id, err))
		return
	}
	if d.Mailbox == nil {
		return
	}
	title := strings.Join(strings.Fields(detail.Story.Title), " ")
	if title == "" {
		title = id
	}
	if _, err := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: MailBlocked + ": " + title,
		Body: fmt.Sprintf("%s (%s) has no files key and its rig expects one, so mw dispatch on %s did not claim it (%s).\n\n%s\n",
			id, title, d.Host, ReasonNeedsFilesKey, fix),
	}); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the mail to %s about its missing files key could not be sent: %v", id, MayorMailbox, err))
	}
}

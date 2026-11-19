package application

import (
	"context"
	"strings"
)

// unreadWithoutApplied is mine less every message a pass already applied (Apply,
// the on-message hook's): the read prints it as one line, "already applied
// earlier: ...", never as a message, so the pane notifier must not type it as
// unread (mw-gq6.1500). An applied comment stays: the read prints its words, the
// Mayor's only sight of them, as printCommentWords does. Like Run, it reads the
// applied notes only when it has a Tracker.
func (i PosternInbox) unreadWithoutApplied(ctx context.Context, mine []PosternInboxMessage) ([]PosternInboxMessage, error) {
	if i.Tracker == nil || len(mine) == 0 {
		return mine, nil
	}
	applied, err := i.appliedNotes(ctx)
	if err != nil || len(applied) == 0 {
		return mine, err
	}
	var unread []PosternInboxMessage
	for _, m := range mine {
		note, done := applied[m.Txid]
		if done && m.Txid != "" && !(strings.HasPrefix(note, "applied comment ") && strings.TrimSpace(m.Text) != "") {
			continue
		}
		unread = append(unread, m)
	}
	return unread, nil
}

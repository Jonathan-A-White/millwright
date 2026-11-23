package application

import (
	"context"
	"strings"
)

// unreadWithoutApplied is mine less every message a pass already applied (Apply,
// the on-message hook's): the read prints it as one line, "already applied
// earlier: ...", never as a message, so the pane notifier must not type it as
// unread (mw-gq6.1500). An applied comment goes too: its words are already a
// comment on its bead, and the Mayor is mailed that, so a count that kept it
// woke the Mayor to read nothing new (mw-gq6.1685). Like Run, it reads the
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
		if appliedTxid(applied, m.Txid) {
			continue
		}
		unread = append(unread, m)
	}
	return unread, nil
}

// nothingNewLine is what a read says first when every message it meets was
// applied earlier, so a Mayor woken by a stale count sees at once that there is
// nothing to do (mw-gq6.1685).
const nothingNewLine = "nothing new: every message here was already applied earlier"

// everyOneApplied reports whether mine is not empty and every message in it
// was already applied.
func everyOneApplied(applied map[string]string, mine []PosternInboxMessage) bool {
	if len(mine) == 0 {
		return false
	}
	for _, m := range mine {
		if !appliedTxid(applied, m.Txid) {
			return false
		}
	}
	return true
}

// appliedNotUnread reports whether message, a post that would end mw talk wait,
// was already applied by a pass, so that the wait does not count it unread, as
// the inbox's count does not (mw-gq6.1685). A voice note stays: its notice
// carries the transcript the pass heard, and the wait is where the Mayor meets
// it. A tracker that cannot be read counts the message, as before.
func (r *talkWaitRun) appliedNotUnread(message PosternInboxMessage) bool {
	if message.Txid == "" || r.Memory == nil {
		return false
	}
	note, err := r.Memory.Note(r.ctx, PosternAppliedKey(message.Txid))
	if err != nil || note == "" {
		return false
	}
	return !strings.HasPrefix(note, "applied voice ")
}

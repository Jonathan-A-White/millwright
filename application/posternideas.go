package application

import (
	"context"
	"strings"
	"time"
)

// ideasPass is what one pass of the inbox, or of mw talk wait, knows of
// Ideas only: read the first time a record of the Governor's turns up, so that
// a pass with nothing of his asks the backend nothing.
type ideasPass struct {
	loaded bool
	state  IdeasOnlyState
	// held is every txid already kept, with the line it is shown as.
	held map[string]string
}

// ideasLoad reads the mode and what is held into p, once. A backend that
// cannot say is said and gone on without: the state file's word stands.
func (i PosternInbox) ideasLoad(ctx context.Context, p *ideasPass) error {
	if p.loaded {
		return nil
	}
	state, err := i.Ideas.Refresh(ctx)
	if err != nil {
		i.printf("%v; going on with the last note of Ideas only\n", err)
	}
	held, err := heldNotes(ctx, i.Memory)
	if err != nil {
		return err
	}
	p.loaded, p.state, p.held = true, state, held
	return nil
}

// holdIdea keeps m, a verified message of the Governor's, when it is already
// kept or the mode is on: it appends m to the held file, marks its txid held
// and answers it in its thread with 'Recorded for after <time>' from the
// Mayor's key, without waking anyone. line is what mw postern inbox shows in
// its place; fresh says it was kept just now and not on an earlier pass. m is
// left alone (kept false) when the mode is off and it was never kept.
func (i PosternInbox) holdIdea(ctx context.Context, m PosternInboxMessage, p *ideasPass) (line string, kept, fresh bool, err error) {
	return i.holdIdeaAnswering(ctx, m, p, nil)
}

// holdIdeaAnswering is holdIdea, answering with answer rather than a message in
// m's thread when answer is not nil: a Talk turn is answered in the talk.
func (i PosternInbox) holdIdeaAnswering(ctx context.Context, m PosternInboxMessage, p *ideasPass, answer func(context.Context, string) error) (line string, kept, fresh bool, err error) {
	if i.Ideas == nil || m.Txid == "" {
		return "", false, false, nil
	}
	if err := i.ideasLoad(ctx, p); err != nil {
		return "", false, false, err
	}
	if line, ok := p.held[m.Txid]; ok {
		return line, true, false, nil
	}
	if !p.state.OnAt(i.Ideas.now()) || i.aboutAToDo(ctx, m) {
		return "", false, false, nil
	}
	file := i.ideaFiles(ctx, m)
	idea := IdeasOnlyHeld{
		Txid: m.Txid, Seq: m.Seq, Class: m.Class, Channel: m.Thread,
		Text: i.ideaWords(ctx, m, file), Attachment: file, When: i.Ideas.now().UTC().Format(time.RFC3339),
	}
	line, err = i.Ideas.keepIdea(ctx, i.Memory, p.state, idea)
	if err != nil {
		return "", false, false, err
	}
	p.held[m.Txid] = line
	text := IdeasOnlyReceipt(p.state, i.Ideas.now())
	if answer != nil {
		err = answer(ctx, text)
	} else {
		err = i.receipt(ctx, m, text)
	}
	if err != nil {
		i.printf("%s was kept, but not answered: %v\n", m.Txid, err)
	}
	return line, true, true, nil
}

// aboutAToDo reports whether m is about one of the Governor's own to-dos, the
// one kind of word of his that Ideas only does not keep: an action or an
// answer naming a bead, or a message in a bead's thread, where that bead is
// labelled hitl:todo. A bead the tracker cannot read or find is not known to
// be one, and its message is kept as an idea.
func (i PosternInbox) aboutAToDo(ctx context.Context, m PosternInboxMessage) bool {
	if i.Tracker == nil {
		return false
	}
	bead := ""
	if reply, ok := decodePosternReply(m.Text); ok {
		bead = reply.Bead
	} else if action, ok := decodePosternAction(m.Text); ok {
		bead = action.Bead
	} else if m.ThreadIsBead {
		bead = m.Thread
	}
	if bead = strings.TrimSpace(bead); bead == "" {
		return false
	}
	found, err := i.Tracker.ShowBeads(ctx, []string{bead})
	if err != nil {
		i.printf("reading %s to tell whether %s is about a to-do: %v; keeping it as an idea\n", bead, m.Txid, err)
		return false
	}
	return len(found) > 0 && found[0].Todo()
}

// ideaWords is what the held line says of m, whose file was written to file: a tap as 'release mw-act.1', a
// voice note as the words heard, else the first 80 runes of his own.
func (i PosternInbox) ideaWords(ctx context.Context, m PosternInboxMessage, file string) string {
	if action, ok := decodePosternAction(m.Text); ok {
		return heldWords(action.Action + " " + action.Bead)
	}
	if i.isVoiceNote(m) {
		if file != "" {
			if heard, err := i.Transcriber.Transcribe(ctx, file); err == nil && strings.TrimSpace(heard) != "" {
				i.keepTranscript(m.Txid, strings.TrimSpace(heard))
				return heldWords(heard)
			}
		}
		return "voice note"
	}
	if words := heldWords(m.Text); words != "" {
		return words
	}
	if len(m.files()) > 0 {
		return "a file"
	}
	return ""
}

// ideaFiles downloads what m carries, if anything, and reports where the first
// file was written; "" when it carries none or could not be downloaded.
func (i PosternInbox) ideaFiles(ctx context.Context, m PosternInboxMessage) string {
	if len(m.files()) == 0 || i.AttachmentDir == "" {
		return ""
	}
	_, saved := i.attachmentOutcome(ctx, m)
	if len(saved) == 0 {
		return ""
	}
	return saved[0].Path
}

// receipt answers m in its own thread, from the Mayor's key, with text. With no
// Sender it answers nothing.
func (i PosternInbox) receipt(ctx context.Context, m PosternInboxMessage, text string) error {
	if i.Sender == nil {
		return nil
	}
	send := PosternSendRequest{Class: "message", Text: text, Re: m.Txid, Recorded: true}
	thread, isBead := m.Thread, m.ThreadIsBead
	if action, ok := decodePosternAction(m.Text); ok {
		thread, isBead = strings.TrimSpace(action.Bead), true
	}
	switch {
	case isBead:
		send.Thread = thread
	case thread != PosternGeneralThread:
		send.Topic = thread
	}
	_, err := i.Sender.Run(ctx, send)
	return err
}

// unreadWithoutHeld is mine less every message kept for Ideas only: a record
// kept is not unread mail, and must not wake the Mayor.
func (i PosternInbox) unreadWithoutHeld(ctx context.Context, mine []PosternInboxMessage) ([]PosternInboxMessage, error) {
	if i.Ideas == nil {
		return mine, nil
	}
	held, err := heldNotes(ctx, i.Memory)
	if err != nil || len(held) == 0 {
		return mine, err
	}
	var unread []PosternInboxMessage
	for _, m := range mine {
		if _, kept := held[m.Txid]; !kept || m.Txid == "" {
			unread = append(unread, m)
		}
	}
	return unread, nil
}

package application

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
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
	if !p.state.OnAt(i.Ideas.now()) {
		return "", false, false, nil
	}
	todo, err := i.aboutAToDo(ctx, m)
	if err != nil {
		return "", false, false, err
	}
	if todo {
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
// one kind of word of his that Ideas only does not keep: a message that begins
// 'TODO:' (beginsTodoColon), whatever its channel; an action or an
// answer naming a bead, or a message in a bead's thread, where that bead is
// labelled hitl:todo; or any other message or Talk turn whose words are about
// a to-do (textAboutAToDo). A bead the tracker cannot find is not known to be
// one, and its message is kept as an idea; a tracker that cannot be read says
// nothing of it, so the error is returned and the message is neither kept nor
// answered on this pass but weighed again on the next (mw-gq6.827).
func (i PosternInbox) aboutAToDo(ctx context.Context, m PosternInboxMessage) (bool, error) {
	if beginsTodoColon(m.Text) {
		return true, nil
	}
	bead := ""
	if reply, ok := decodePosternReply(m.Text); ok {
		bead = reply.Bead
	} else if action, ok := decodePosternAction(m.Text); ok {
		bead = action.Bead
	} else if m.ThreadIsBead {
		bead = m.Thread
	} else {
		return i.textAboutAToDo(ctx, m)
	}
	if bead = strings.TrimSpace(bead); bead == "" || i.Tracker == nil {
		return false, nil
	}
	found, err := i.Tracker.ShowBeads(ctx, []string{bead})
	if err != nil {
		return false, fmt.Errorf("reading %s to tell whether %s is about a to-do: %w; weighing it again on the next pass", bead, m.Txid, err)
	}
	return len(found) > 0 && found[0].Todo(), nil
}

// beginsTodoColon reports whether text begins with 'TODO:', in any case, after
// any blanks: the Governor's way of asking that a message be looked at at once
// though Ideas only is on (mw-8ifimx.18).
func beginsTodoColon(text string) bool {
	text = strings.TrimSpace(text)
	return len(text) >= 5 && strings.EqualFold(text[:5], "todo:")
}

// textAboutAToDo reports whether the words of m, a Talk turn or a free-text
// message, are about a to-do: they say 'to do', 'to-do' or 'todo', name the id
// of an open hitl:todo bead, or share a distinctive word with the title of one
// or of its list. The tracker is read only when the words do not say 'to do'
// themselves; a tracker that cannot be read is an error, and the message is
// weighed again on the next pass (mw-gq6.827).
func (i PosternInbox) textAboutAToDo(ctx context.Context, m PosternInboxMessage) (bool, error) {
	words := todoWords(m.Text)
	if len(words) == 0 {
		return false, nil
	}
	for n, w := range words {
		if w == "todo" || w == "todos" || (w == "to" && n+1 < len(words) && (words[n+1] == "do" || words[n+1] == "dos")) {
			return true, nil
		}
	}
	if i.Tracker == nil {
		return false, nil
	}
	cards, err := openTodoCards(ctx, i.Tracker, i.Graph)
	if err != nil {
		return false, fmt.Errorf("reading the to-dos to tell whether %s is about one: %w; weighing it again on the next pass", m.Txid, err)
	}
	have := map[string]bool{}
	for _, w := range words {
		have[w] = true
	}
	lower := strings.ToLower(m.Text)
	var epics []string
	seen := map[string]bool{}
	for _, card := range cards {
		if card.IsEpic || !card.Todo() {
			continue
		}
		if namesBeadID(lower, strings.ToLower(card.Story.ID)) || titleSharesWord(have, card.Story.Title) {
			return true, nil
		}
		if card.EpicID != "" && !seen[card.EpicID] {
			seen[card.EpicID] = true
			epics = append(epics, card.EpicID)
		}
	}
	if len(epics) == 0 {
		return false, nil
	}
	lists, err := i.Tracker.ShowBeads(ctx, epics)
	if err != nil {
		return false, fmt.Errorf("reading the lists of the to-dos to tell whether %s is about one: %w; weighing it again on the next pass", m.Txid, err)
	}
	for _, list := range lists {
		if titleSharesWord(have, list.Story.Title) {
			return true, nil
		}
	}
	return false, nil
}

// todoWords is text in lower case, split at everything that is not a letter or
// a digit.
func todoWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// namesBeadID reports whether lower, a message in lower case, names the bead id
// (also in lower case) as a whole: mw-e.3 is not named by mw-e.30 or mw-e.3.1.
func namesBeadID(lower, id string) bool {
	if id == "" {
		return false
	}
	for from := 0; ; {
		at := strings.Index(lower[from:], id)
		if at < 0 {
			return false
		}
		start, end := from+at, from+at+len(id)
		before := start == 0 || !isIDRune(rune(lower[start-1]))
		after := end == len(lower) || !isIDRune(rune(lower[end])) && !(lower[end] == '.' && end+1 < len(lower) && isIDRune(rune(lower[end+1])))
		if before && after {
			return true
		}
		from = start + 1
	}
}

func isIDRune(r rune) bool {
	return r == '-' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// titleSharesWord reports whether a distinctive word of title is among have:
// five letters or more, and not one of the plain words in commonTitleWords.
func titleSharesWord(have map[string]bool, title string) bool {
	for _, w := range todoWords(title) {
		if len(w) >= 5 && !commonTitleWords[w] && have[w] {
			return true
		}
	}
	return false
}

// commonTitleWords are words of a to-do's title that say nothing of which
// to-do it is, and so tie a message to none.
var commonTitleWords = map[string]bool{
	"about": true, "after": true, "before": true, "check": true, "could": true, "every": true, "first": true,
	"going": true, "other": true, "please": true, "should": true, "their": true, "there": true,
	"these": true, "thing": true, "things": true, "think": true, "those": true, "today": true,
	"which": true, "while": true, "would": true, "write": true, "watch": true, "start": true,
	"story": true, "never": true, "again": true, "being": true, "where": true, "idea": true, "ideas": true,
	"help": true, "week": true, "needs": true, "make": true, "send": true, "email": true,
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

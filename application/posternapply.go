package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// PosternAppliedPrefix is the prefix of every note PosternAppliedKey writes.
const PosternAppliedPrefix = "postern.applied."

// PosternAppliedKey is the note key a message's txid is marked under once
// the Governor's word it carries has been applied — or refused — at zero
// tokens (postern's docs/protocol.md section 13: each at most once per
// txid). Its value is the one line mw postern inbox shows in the message's
// place.
func PosternAppliedKey(txid string) string { return PosternAppliedPrefix + txid }

// The Governor's actions, postern's docs/protocol.md section 13.
const (
	PosternActionRelease  = "release"
	PosternActionHold     = "hold"
	PosternActionPriority = "priority"
	PosternActionThaw     = "thaw"
	PosternActionVerified = "verified"
	PosternActionKeep     = "keep"
	PosternActionClose    = "close"

	// The three answers to a chase need (protocol section 13).
	PosternActionChase       = "chase"
	PosternActionAskDone     = "ask_done"
	PosternActionKeepWaiting = "keep_waiting"

	// The three hand edits to his To do board (protocol section 13). Each is
	// applied to hitl:todo beads only.
	PosternActionTodoAdd  = "todo_add"
	PosternActionTodoEdit = "todo_edit"
	PosternActionTodoMove = "todo_move"
)

// PosternKeepDays is how many days a keep action that names none keeps a
// stale bead for.
const PosternKeepDays = 30

// PosternAction is an action's plaintext, section 13: a JSON object naming
// what to do and the bead to do it to, and — for a priority — the priority.
type PosternAction struct {
	Action   string `json:"action"`
	Bead     string `json:"bead"`
	Priority *int   `json:"priority,omitempty"`

	// Days is a keep's: how many days more the bead is kept, PosternKeepDays
	// when it names none.
	Days *int `json:"days,omitempty"`

	// List, Title, Notes, Before and After are the To do board actions': the
	// list (an epic) a card is added to or moved to, the title and notes a card
	// is added or edited with (a nil one is left as it is), and the card a moved
	// card is put before or after. A todo_add names its list in List, and Bead
	// is read as that list.
	List  string  `json:"list,omitempty"`
	Title *string `json:"title,omitempty"`
	Notes *string `json:"notes,omitempty"`
	// Due is a todo_edit's new due time, RFC 3339; an empty one clears it, and
	// a nil one leaves it as it is.
	Due *string `json:"due,omitempty"`
	// Important is a todo_edit's star: true marks the card important, false
	// takes it off, and a nil one leaves it as it is.
	Important *bool `json:"important,omitempty"`
	// Size is a todo_edit's new size (quick, hour or half-day) and Then its new
	// Then line; an empty one clears it, and a nil one leaves it as it is.
	Size   *string `json:"size,omitempty"`
	Then   *string `json:"then,omitempty"`
	Before string  `json:"before,omitempty"`
	After  string  `json:"after,omitempty"`

	// Step, SHA256, ApprovedAt and Sig are a run action's (§17): the step
	// approved, its hash as he saw it, when he approved it (Unix seconds),
	// and his key's signature over both.
	Step       string `json:"step,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	ApprovedAt int64  `json:"approved_at,omitempty"`
	Sig        string `json:"sig,omitempty"`
}

// decodePosternAction reads text as a PosternAction, reporting false when it
// is not a JSON object with a non-empty action and bead.
func decodePosternAction(text string) (PosternAction, bool) {
	var action PosternAction
	if err := json.Unmarshal([]byte(text), &action); err != nil {
		return PosternAction{}, false
	}
	action.Action = strings.ToLower(strings.TrimSpace(action.Action))
	if action.Action == PosternActionTodoAdd {
		action.Bead = action.List
	}
	if action.Action == "" || strings.TrimSpace(action.Bead) == "" {
		return PosternAction{}, false
	}
	return action, true
}

// knownPosternAction reports whether this host knows how to apply action. One
// it does not is left for the Mayor to read as text.
func knownPosternAction(action string) bool {
	switch action {
	case PosternActionRelease, PosternActionHold, PosternActionPriority, PosternActionThaw, PosternActionVerified, PosternActionRun,
		PosternActionKeep, PosternActionClose, PosternActionChase, PosternActionAskDone, PosternActionKeepWaiting,
		PosternActionTodoAdd, PosternActionTodoEdit, PosternActionTodoMove:
		return true
	}
	return false
}

// posternApplied is what applying one message did: its kind (answer, comment,
// voice, or an action's name), the bead or thread it was about, its txid,
// and — when it was refused, or when there is something the Mayor must see
// that lives nowhere else — why, or what.
type posternApplied struct {
	Kind, Bead, Txid string
	Refused          bool
	Detail           string
	// Said, when set, is the whole summary: a prompt call is printed as a call.
	Said string
	// pictures is true when a file that is not audio was saved with the message.
	pictures bool
}

// summary is the line a pass prints for it: never a message's text.
func (a posternApplied) summary() string {
	if a.Said != "" {
		return a.Said
	}
	verb := "applied"
	if a.Refused {
		verb = "refused"
	}
	return fmt.Sprintf("%s %s %s txid %s", verb, a.Kind, a.Bead, a.Txid)
}

// line is the one line mw postern inbox shows in the message's place, and
// what PosternAppliedKey holds: the summary, with why it was refused or what
// else the Mayor must see.
func (a posternApplied) line() string {
	line := a.summary()
	if a.Detail != "" {
		line += ": " + strings.Join(strings.Fields(a.Detail), " ")
	}
	return line
}

// Apply is the zero-token pass the postern backend's on-message hook runs
// (mw postern inbox --apply): everything indexed since the cursor that the
// Governor verifiably sent and this host knows how to apply is applied at
// once — a reply to a question (section 6), a comment in a bead's thread, an
// action (section 13), a voice note (section 14) — each at most once per
// txid, marked under PosternAppliedKey, commented on its bead and mailed to
// the Mayor. It never moves the cursor, so the Mayor's own read still sees
// every message, and never prints what a message says: only one line per
// message applied. Anything else — another sender, an action this host does
// not know, a message with no txid to mark — is left for the Mayor.
//
// A move-home (§18) is applyMoveHome's: run here when the Governor signed it
// and it names this host, refused when he did not sign it or it is a replay.
// On a host the vault's home file says is not home — a boost — a move-home is
// all a pass applies, so that nothing the home's own pass records is recorded
// twice, and a tracker the boost cannot reach (the old home's, dead) does not
// stop it: see toApply.
//
// A picture it saved is matched (mw verify match) as the follower's pass matches
// one, and Apply returns once that match has ended, bounded by the hook's own
// timeout: this pass is a process that ends with it, and the backend's hook
// applies a picture before the follower's pass sees it (mw-4xdnis.6). A match
// that fails or runs past its bound is said by the hook and changes nothing of
// what Apply returns. A txid matched here is applied, so the follower's pass
// never meets it again.
func (i PosternInbox) Apply(ctx context.Context) ([]string, error) {
	lines, _, err := i.apply(ctx, false, 0)
	if i.Pictures != nil {
		i.Pictures.Wait()
	}
	return lines, err
}

// isOneTap reports whether m is one of the Governor's one-tap actions the
// follower applies on its own: an action this host knows (section 13) other
// than a hands step's run, which stays with the pass that can outlive a
// restart of the backend. A move-home, a reply, a comment and a voice note are
// none of them.
func isOneTap(m PosternInboxMessage) bool {
	if m.Class == PosternClassMoveHome {
		return false
	}
	if _, ok := decodePosternReply(m.Text); ok {
		return false
	}
	action, ok := decodePosternAction(m.Text)
	return ok && knownPosternAction(action.Action) && action.Action != PosternActionRun
}

// apply is Apply, or with tapsOnly the follower's pass: only the Governor's
// one-tap actions are applied, by the same applyOne, each marked, answered and
// echoed as Apply does, and everything else is left unread and unapplied for
// the seat's own pass. Ideas are not given back, and a host that is not home
// applies none. Records are read from after, when that is past the inbox's
// cursor, so a follower does not read what it has read again; resume is the
// seq to read from next time, held back at a tap that could not be applied yet.
func (i PosternInbox) apply(ctx context.Context, tapsOnly bool, after int64) (lines []string, resume int64, err error) {
	resume = after
	if err := i.wired(); err != nil {
		return nil, resume, err
	}
	i.prompts = &promptCache{}
	if i.Tracker == nil {
		return nil, resume, fmt.Errorf("mw postern inbox --apply: no work tracker is configured to apply anything to")
	}
	release, err := i.lock(ctx)
	if err != nil {
		return nil, resume, err
	}
	defer release()
	// The pictures saved are matched once the pass has ended, apart from it:
	// the pass never waits on the match (mw-4xdnis.4).
	if i.Pictures != nil {
		defer i.Pictures.Flush(ctx)
	}

	home, err := i.homeHost(ctx)
	if err != nil {
		return nil, resume, err
	}
	pass := moveHomePass{home: home}
	if tapsOnly && pass.onBoost(i.Host) {
		return nil, resume, nil
	}
	// A mode that has ended gives its ideas back before anything of his is read,
	// so that what he sends now is not kept with them.
	if !tapsOnly && !pass.onBoost(i.Host) {
		lines = i.giveIdeasBack(ctx)
	}
	mine, applied, newest, err := i.toApply(ctx, &pass, after)
	if err != nil {
		return nil, resume, err
	}
	pass.applied = applied
	i.keepMessages(mine)
	stuck := int64(0)
	var ideas ideasPass
	for _, m := range mine {
		if tapsOnly && !isOneTap(m) && !i.isPictureNote(m) {
			continue
		}
		if m.Txid == "" {
			continue
		}
		if _, done := applied[m.Txid]; done {
			continue
		}
		if m.Class == PosternClassMoveHome {
			if i.HomeMover == nil {
				continue
			}
			if result, handled := i.applyMoveHome(ctx, m, pass); handled {
				lines = append(lines, result.summary())
				i.printf("%s\n", result.line())
			}
			continue
		}
		if pass.onBoost(i.Host) || !m.Verified || !i.isGovernor(m) {
			continue
		}
		// Ideas only: his word is kept, not applied. A move-home above is
		// infrastructure and does not wait for him.
		line, kept, fresh, err := i.holdIdea(ctx, m, &ideas)
		if err != nil {
			return lines, after, err
		}
		if kept {
			if fresh {
				lines = append(lines, line)
				i.printf("%s\n", line)
			}
			continue
		}
		result, outcome, handled, err := i.applySavingFiles(ctx, m)
		if err != nil {
			return lines, after, err
		}
		if !handled {
			if stuck == 0 || m.Seq < stuck {
				stuck = m.Seq
			}
			continue
		}
		if err := i.markApplied(ctx, applied, result); err != nil {
			return lines, after, err
		}
		if result.pictures && i.Pictures != nil {
			i.Pictures.Saved(result.Txid)
		}
		lines = append(lines, result.summary())
		i.printf("%s\n", result.summary())
		if outcome != "" && !m.ThreadIsBead && !i.isVoiceNote(m) {
			i.printf("%s\n", outcome)
		}
	}
	resume = max(after, newest)
	if stuck > 0 && stuck-1 < resume {
		resume = max(after, stuck-1)
	}
	return lines, resume, nil
}

// toApply reads what a pass may apply and the txids already applied. On the
// home a failure to read either is the pass's failure. On a boost, whose beads
// server is the home's and may be dead, each read is bounded by
// PosternBoostWait, and one that fails is said and set aside rather than
// stopping the pass: every record is read from the start, none is taken for
// applied, and the pass is dark (a move-home then writes nothing before its
// move), so that a move the Governor asked for is never held up by the host
// it moves away from.
func (i PosternInbox) toApply(ctx context.Context, pass *moveHomePass, after int64) ([]PosternInboxMessage, map[string]string, int64, error) {
	if !pass.onBoost(i.Host) {
		cursor, err := i.readCursor(ctx)
		if err != nil {
			return nil, nil, 0, err
		}
		mine, newest, err := i.fetchSince(ctx, max(cursor, after))
		if err != nil {
			return nil, nil, 0, err
		}
		if len(mine) == 0 {
			// Nothing to tell applied from not: the read of the applied notes is a
			// call to the beads server on every pass of the follower (mw-gq6.596).
			return mine, map[string]string{}, newest, nil
		}
		applied, err := i.appliedNotes(ctx)
		return mine, applied, newest, err
	}
	bounded, cancel := i.bounded(ctx, *pass)
	cursor, err := i.readCursor(bounded)
	cancel()
	if err != nil {
		i.printf("this host is not home and the postern inbox cursor could not be read (%v): every record is read for a move-home\n", err)
		cursor, pass.dark = 0, true
	}
	mine, newest, err := i.fetchSince(ctx, cursor)
	if err != nil {
		return nil, nil, 0, err
	}
	applied := map[string]string{}
	if !pass.dark {
		bounded, cancel := i.bounded(ctx, *pass)
		defer cancel()
		if applied, err = i.appliedNotes(bounded); err != nil {
			i.printf("this host is not home and %v: none is taken for applied\n", err)
			applied, pass.dark = map[string]string{}, true
		}
	}
	return mine, applied, newest, nil
}

// appliedNotes reads every PosternAppliedKey note at once, txid to line.
func (i PosternInbox) appliedNotes(ctx context.Context) (map[string]string, error) {
	notes, err := i.Memory.NotesWithPrefix(ctx, PosternAppliedPrefix)
	if err != nil {
		return nil, fmt.Errorf("reading which postern messages are already applied: %w", err)
	}
	applied := make(map[string]string, len(notes))
	for key, line := range notes {
		applied[strings.TrimPrefix(key, PosternAppliedPrefix)] = line
	}
	return applied, nil
}

// markApplied notes result's txid applied, and remembers it in applied for
// the rest of this run. A message with no txid has nothing to mark.
func (i PosternInbox) markApplied(ctx context.Context, applied map[string]string, result posternApplied) error {
	if result.Txid == "" {
		return nil
	}
	if err := i.Memory.SetNote(ctx, PosternAppliedKey(result.Txid), result.line()); err != nil {
		return fmt.Errorf("marking %s applied: %w", result.Txid, err)
	}
	applied[result.Txid] = result.line()
	return nil
}

// applySavingFiles is applyOne for a message whose files are saved first: the
// one place a Governor message is applied, so that no route marks a message
// applied with its files unsaved (mw-gq6.686). outcome is the lines a reader is
// shown of the files' download, "" when the message carries none.
func (i PosternInbox) applySavingFiles(ctx context.Context, m PosternInboxMessage) (result posternApplied, outcome string, handled bool, err error) {
	var saved []posternSavedFile
	if len(m.files()) > 0 {
		outcome, saved = i.attachmentOutcome(ctx, m)
	}
	result, handled, err = i.applyOne(ctx, m, outcome, saved)
	for _, file := range saved {
		result.pictures = result.pictures || !isPosternAudio(file.Mime)
	}
	return result, outcome, handled, err
}

// isPictureNote reports whether m is a message of the Governor's that the
// follower applies on its own beside his one-taps: words and pictures on the
// general thread or a named channel, which applyOne only saves and notes, so a
// picture is saved and matched (mw verify match) within seconds, not when the
// inbox is next run. A reply, an action, a voice note and anything on a bead's
// thread stay for the pass that comments and mails.
func (i PosternInbox) isPictureNote(m PosternInboxMessage) bool {
	if i.Pictures == nil || m.Class == PosternClassMoveHome || m.ThreadIsBead || i.isVoiceNote(m) {
		return false
	}
	if _, ok := decodePosternReply(m.Text); ok {
		return false
	}
	if _, ok := decodePosternAction(m.Text); ok {
		return false
	}
	for _, file := range m.files() {
		if file != nil && !isPosternAudio(file.Mime) {
			return true
		}
	}
	return false
}

// applyOne applies one verified message from the Governor, reporting what it
// did and whether it was one to apply at all. outcome and saved are its
// attachments' download, when it has any and they were downloaded: the lines
// a reader is shown, and each file written.
func (i PosternInbox) applyOne(ctx context.Context, m PosternInboxMessage, outcome string, saved []posternSavedFile) (posternApplied, bool, error) {
	if reply, ok := decodePosternReply(m.Text); ok {
		recorded, err := i.recordAnswer(ctx, m, reply)
		if err != nil || !recorded {
			return posternApplied{}, false, err
		}
		i.answerByActs(ctx, m, i.actedBeads(ctx, reply.Bead)...)
		return posternApplied{Kind: "answer", Bead: reply.Bead, Txid: m.Txid}, true, nil
	}
	if action, ok := decodePosternAction(m.Text); ok {
		// BRC-78 carries no replay protection (postern's docs/protocol.md
		// section 1): an action is written as the Governor only when the
		// backend vouched for the key that delivered or signed its record —
		// a direct record's authenticated key, a transaction's signing key —
		// so an old action carried again by someone else is never applied.
		// One whose signer is unchecked is left for the Mayor to read.
		if !knownPosternAction(action.Action) || !m.SignerChecked || (action.Action == PosternActionRun && !i.runsHands()) {
			return posternApplied{}, false, nil
		}
		result, err := i.applyAction(ctx, m, action)
		if err == nil && !result.Refused && action.Action != PosternActionRun {
			i.answerByActs(ctx, m, i.actedBeads(ctx, action.Bead)...)
			i.echoAction(ctx, action, m.Txid)
		}
		return result, err == nil, err
	}
	if i.isVoiceNote(m) {
		// A voice note is one single attachment, so at most one file.
		path := ""
		if len(saved) > 0 {
			path = saved[0].Path
		}
		result, err := i.applyVoice(ctx, m, outcome, path)
		return result, err == nil, err
	}
	if result, handled, err := i.applyPromptCall(ctx, m); handled || err != nil {
		return result, handled && err == nil, err
	}
	if m.ThreadIsBead {
		if result, handled, err := i.applyLooksGood(ctx, m); handled || err != nil {
			return result, handled && err == nil, err
		}
		recorded, fresh, err := i.recordThreadCommentOnce(ctx, m, saved)
		if err != nil || !recorded {
			return posternApplied{}, false, err
		}
		if !fresh {
			return posternApplied{Kind: "comment", Bead: m.Thread, Txid: m.Txid}, true, nil
		}
		if err := i.mail(ctx, fmt.Sprintf("Governor on %s: %s", m.Thread, clippedTo(strings.Join(strings.Fields(m.Text), " "), 60)),
			fmt.Sprintf("The Governor by postern %s, txid %s, on %s:\n\n%s%s", sentInFull(m.Ts), m.Txid, m.Thread, m.Text+picturesSuffix(m, saved), answerSuffix(m))); err != nil {
			return posternApplied{}, false, err
		}
		return posternApplied{Kind: "comment", Bead: m.Thread, Txid: m.Txid, Detail: filesDetail(outcome, saved)}, true, nil
	}
	if len(m.files()) > 0 {
		// Words on the general thread or a named channel have no bead to be
		// commented on, but the files he sends are saved and their paths recorded;
		// the Mayor's read shows the applied line and his whole text.
		return posternApplied{Kind: "comment", Bead: m.Thread, Txid: m.Txid, Detail: filesDetail(outcome, saved)}, true, nil
	}
	return posternApplied{}, false, nil
}

// picturesSuffix is what a mail of m says of its pictures, the files that are
// not audio: how many, and the path of each one saved. "" when it carries none.
func picturesSuffix(m PosternInboxMessage, saved []posternSavedFile) string {
	pictures := 0
	for _, file := range m.files() {
		if file != nil && !isPosternAudio(file.Mime) {
			pictures++
		}
	}
	var paths []string
	for _, file := range saved {
		if !isPosternAudio(file.Mime) {
			paths = append(paths, file.Path)
		}
	}
	what := strings.Join(paths, ", ")
	if len(paths) < pictures {
		what = strings.TrimPrefix(what+fmt.Sprintf(", %d not saved", pictures-len(paths)), ", ")
	}
	return carriesPictures(pictures, what)
}

// filesDetail is what an applied message's note says of its files: each one
// written, and each one refused, in the words attachmentOutcome printed.
func filesDetail(outcome string, saved []posternSavedFile) string {
	detail := posternSavedNames(saved)
	for _, line := range strings.Split(outcome, "\n") {
		written := false
		for _, file := range saved {
			written = written || strings.HasPrefix(line, file.Path)
		}
		if line != "" && !written {
			detail = strings.TrimSpace(detail + " " + line)
		}
	}
	return detail
}

// isLooksGood reports whether text is only the words "looks good": trimmed,
// in any case, with one final '.' or '!' allowed. The Postern app's Looks good
// tap on a demo card sends exactly these words into the demo's channel.
func isLooksGood(text string) bool {
	text = strings.TrimSpace(text)
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, "."), "!"))
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") {
		return false
	}
	return strings.EqualFold(strings.Join(strings.Fields(text), " "), "looks good")
}

// isDemo reports whether bead is one the Governor is to be shown working: it
// carries LabelDemo, or its title starts "Demo" as the demo stories' do.
func isDemo(bead StoryDetail) bool {
	title := strings.TrimSpace(bead.Story.Title)
	return hasLabel(bead.Labels, LabelDemo) || strings.HasPrefix(title, "Demo") || strings.HasPrefix(title, "DEMO")
}

// applyLooksGood closes a demo on the Governor's "Looks good", as the close
// action closes a story: m is a message in a bead's channel whose whole text
// is those words, and the bead is an open, unclaimed demo. Any other message,
// or bead, is not its: it reports false and changes nothing, so the message
// takes the ordinary path.
func (i PosternInbox) applyLooksGood(ctx context.Context, m PosternInboxMessage) (posternApplied, bool, error) {
	if i.Tracker == nil || !isLooksGood(m.Text) {
		return posternApplied{}, false, nil
	}
	found, err := i.Tracker.ShowBeads(ctx, []string{m.Thread})
	if err != nil {
		return posternApplied{}, false, fmt.Errorf("reading %s for the Governor's Looks good: %w", m.Thread, err)
	}
	if len(found) == 0 {
		return posternApplied{}, false, nil
	}
	bead := found[0]
	if bead.IsEpic || bead.Closed() || isClaimed(bead) || !isDemo(bead) {
		return posternApplied{}, false, nil
	}
	reason := fmt.Sprintf("Demo accepted on the Governor's 'Looks good' (txid %s)", m.Txid)
	comment := fmt.Sprintf("The Governor by postern %s, on the demo: %q (txid %s). %s", sentInFull(m.Ts), strings.TrimSpace(m.Text), m.Txid, reason)
	// The comment first, as applyClose writes it: a close that fails leaves no
	// bead the Governor's word is not on.
	if err := i.Tracker.CommentOnStory(ctx, bead.Story.ID, comment); err != nil {
		return posternApplied{}, false, fmt.Errorf("recording the Governor's Looks good on %s: %w", bead.Story.ID, err)
	}
	result := posternApplied{Kind: PosternActionClose, Bead: bead.Story.ID, Txid: m.Txid}
	if err := i.Tracker.CloseStory(ctx, bead.Story.ID, reason); err != nil {
		result.Refused, result.Detail = true, fmt.Sprintf("closing %s failed: %v", bead.Story.ID, err)
		return result, true, i.mail(ctx, fmt.Sprintf("Not applied: Looks good on %s", bead.Story.ID),
			fmt.Sprintf("The Governor said Looks good by postern (txid %s) to %s; not applied: %s.", m.Txid, bead.Story.ID, result.Detail))
	}
	return result, true, i.mail(ctx, fmt.Sprintf("Closed: %s on his Looks good", bead.Story.ID), comment)
}

// echoAction appends the event that says an action of the Governor's was
// applied, its detail the tap's txid, so the app that sent the tap can tell
// from the events tail that it took. Release, Hold, Keep, Close, Verified and
// the three answers to a chase need are echoed; a priority is not. It is written after the action is done, so a
// failure to write it is said, not returned: the action stands, and applying
// the tap again would only double it.
func (i PosternInbox) echoAction(ctx context.Context, action PosternAction, txid string) {
	switch action.Action {
	case PosternActionRelease, PosternActionHold, PosternActionKeep, PosternActionClose, PosternActionVerified,
		PosternActionChase, PosternActionAskDone, PosternActionKeepWaiting,
		PosternActionTodoAdd, PosternActionTodoEdit, PosternActionTodoMove:
	default:
		return
	}
	if i.Events == nil {
		return
	}
	if _, err := (EventEmit{Log: i.Events, Now: i.now, Event: events.Event{
		Kind: events.KindActionApplied, Bead: action.Bead, Actor: GovernorPosternActor, Detail: txid,
	}}).Run(ctx); err != nil {
		i.printf("not echoed: %s %s txid %s: %v\n", action.Action, action.Bead, txid, err)
	}
}

// applyAction applies one of the Governor's section 13 actions to its bead,
// as the Governor, at zero tokens: release, hold, priority, verified, keep,
// close, or one of the three answers to a chase need. Each done is commented on its bead and mailed to the Mayor; each that
// cannot be done — no such bead, a hold on a story already claimed, a priority
// out of range — is refused, and the Mayor is mailed why. Either way it is
// applied, so it is never tried again.
func (i PosternInbox) applyAction(ctx context.Context, m PosternInboxMessage, action PosternAction) (posternApplied, error) {
	if action.Action == PosternActionRun {
		return i.applyRun(ctx, m, action)
	}
	result := posternApplied{Kind: action.Action, Bead: action.Bead, Txid: m.Txid}
	refuse := func(why string) (posternApplied, error) {
		result.Refused, result.Detail = true, why
		return result, i.mail(ctx, fmt.Sprintf("Not applied: %s %s", action.Action, action.Bead),
			fmt.Sprintf("The Governor asked, by postern (txid %s), to %s %s; not applied: %s.", m.Txid, action.Action, action.Bead, why))
	}
	done := func(subject, comment string) (posternApplied, error) {
		if err := i.Tracker.CommentOnStory(ctx, action.Bead, comment); err != nil {
			return posternApplied{}, fmt.Errorf("recording the Governor's %s on %s: %w", action.Action, action.Bead, err)
		}
		return result, i.mail(ctx, subject, comment)
	}

	found, err := i.Tracker.ShowBeads(ctx, []string{action.Bead})
	if err != nil {
		return posternApplied{}, fmt.Errorf("reading %s for the Governor's %s: %w", action.Bead, action.Action, err)
	}
	if len(found) == 0 {
		return refuse(fmt.Sprintf("there is no bead %s", action.Bead))
	}
	bead := found[0]

	switch action.Action {
	case PosternActionRelease:
		if bead.IsEpic {
			plan, err := Release{Tracker: i.Tracker}.Run(ctx, action.Bead)
			if err != nil {
				return refuse(err.Error())
			}
			held := plan.held()
			if len(held) == 0 {
				return refuse("it has no held stories")
			}
			return done(fmt.Sprintf("Released: %s", action.Bead),
				fmt.Sprintf("RELEASED by the Governor via postern, txid %s: %d held stories, ready: %s", m.Txid, len(held), joinOrNone(plan.ready())))
		}
		if bead.Iced() {
			return refuse("it is iced; thaw it first")
		}
		if !bead.Held() {
			return refuse(fmt.Sprintf("it is %s, not held", bead.Status))
		}
		if err := i.Tracker.ReleaseStory(ctx, action.Bead); err != nil {
			return refuse(err.Error())
		}
		return done(fmt.Sprintf("Released: %s", action.Bead), fmt.Sprintf("RELEASED by the Governor via postern, txid %s", m.Txid))

	case PosternActionHold:
		if bead.IsEpic {
			return refuse("it is an epic; hold its stories one by one")
		}
		if i.Events != nil && strings.EqualFold(strings.TrimSpace(bead.Status), StatusInProgress) && strings.TrimSpace(bead.Assignee) != "" {
			// A claimed story has a session at work: the hold is a cancel, and
			// ending the session, giving the claim back and holding the story
			// are the follower's, which a bead written here would only race.
			cancel, err := EventEmit{Log: i.Events, Now: i.now, Event: events.Event{
				Kind: events.KindControl, Bead: action.Bead, Actor: GovernorPosternActor, Detail: events.ControlCancel,
			}}.Run(ctx)
			if err != nil {
				return refuse(fmt.Sprintf("it is claimed%s, and its cancel could not be written: %v", claimedBy(bead.Assignee), err))
			}
			return done(fmt.Sprintf("Held: %s", action.Bead), fmt.Sprintf(
				"HELD: by the Governor via postern, txid %s: the story is claimed%s, so its session is being cancelled (control event %d)",
				m.Txid, claimedBy(bead.Assignee), cancel.Seq))
		}
		if !strings.EqualFold(strings.TrimSpace(bead.Status), StatusOpen) || strings.TrimSpace(bead.Assignee) != "" {
			return refuse(fmt.Sprintf("it is %s%s, not open and unclaimed", bead.Status, claimedBy(bead.Assignee)))
		}
		if err := i.Tracker.HoldStory(ctx, action.Bead); err != nil {
			return refuse(err.Error())
		}
		return done(fmt.Sprintf("Held: %s", action.Bead), fmt.Sprintf("HELD: by the Governor via postern, txid %s", m.Txid))

	case PosternActionPriority:
		if action.Priority == nil {
			return refuse("it names no priority")
		}
		if err := ValidPriority(*action.Priority); err != nil {
			return refuse(err.Error())
		}
		if err := i.Tracker.SetStoryPriority(ctx, action.Bead, *action.Priority); err != nil {
			return refuse(err.Error())
		}
		return done(fmt.Sprintf("Priority %d: %s", *action.Priority, action.Bead),
			fmt.Sprintf("PRIORITY %d set by the Governor via postern, txid %s", *action.Priority, m.Txid))

	case PosternActionThaw:
		if bead.IsEpic {
			return refuse("it is an epic; thaw its stories one by one")
		}
		comment := fmt.Sprintf("Thawed by the Governor via postern, txid %s", m.Txid)
		if err := (Thaw{Tracker: i.Tracker, Notes: i.Memory, Now: i.now, Comment: comment}).Run(ctx, action.Bead, action.Priority); err != nil {
			return refuse(err.Error())
		}
		return result, i.mail(ctx, fmt.Sprintf("Thawed: %s", action.Bead), comment)

	case PosternActionKeep:
		days := PosternKeepDays
		if action.Days != nil {
			days = *action.Days
		}
		if days < 1 {
			return refuse(fmt.Sprintf("it keeps the bead for %d days", days))
		}
		until := i.now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		if err := i.Memory.SetNote(ctx, PosternKeepKey(action.Bead), until.Format(time.RFC3339)); err != nil {
			return posternApplied{}, fmt.Errorf("keeping %s for the Governor: %w", action.Bead, err)
		}
		return done(fmt.Sprintf("Kept: %s", action.Bead),
			fmt.Sprintf("KEPT by the Governor via postern (%s) until %s", m.Txid, until.Format("2006-01-02")))

	case PosternActionClose:
		return i.applyClose(ctx, m, bead, refuse, done)

	case PosternActionTodoAdd:
		return i.applyTodoAdd(ctx, m, action, bead, refuse, result)

	case PosternActionTodoEdit:
		return i.applyTodoEdit(ctx, action, bead, refuse, done)

	case PosternActionTodoMove:
		return i.applyTodoMove(ctx, action, bead, refuse, done)

	case PosternActionChase, PosternActionAskDone, PosternActionKeepWaiting:
		return i.applyChaseAnswer(ctx, m, action.Action, bead, refuse, done)

	default: // PosternActionVerified
		comments, err := i.Tracker.StoryComments(ctx, action.Bead)
		if err != nil {
			return posternApplied{}, fmt.Errorf("reading %s's comments for the Governor's verified: %w", action.Bead, err)
		}
		for _, c := range comments {
			if commentMarksVerified(c.Text) {
				return refuse("it is already verified")
			}
		}
		return done(fmt.Sprintf("Verified: %s", action.Bead), fmt.Sprintf("VERIFIED by the Governor via postern (%s)", m.Txid))
	}
}

// applyChaseAnswer applies one of the three answers to a chase need to bead,
// which must be open and waiting on others. ask_done does what mw ask done
// does. chase and keep_waiting both begin the wait again from now, so the next
// chase need falls due ChaseAfterWorkingDays working days on and the waiting
// mark stays; the Governor chasing the person and his choosing to wait longer
// differ only in the comment that records them.
func (i PosternInbox) applyChaseAnswer(ctx context.Context, m PosternInboxMessage, name string, bead StoryDetail,
	refuse func(string) (posternApplied, error), done func(subject, comment string) (posternApplied, error)) (posternApplied, error) {
	if bead.Closed() {
		return refuse("it is already closed")
	}
	if !bead.WaitingOnOthers() {
		return refuse("it is not waiting on others")
	}
	id := bead.Story.ID
	if name == PosternActionAskDone {
		if err := (Ask{Tracker: i.Tracker, Notes: i.Memory}).Done(ctx, id); err != nil {
			return refuse(err.Error())
		}
		return done(fmt.Sprintf("Done waiting: %s", id),
			fmt.Sprintf("WAITING ENDED by the Governor via postern, txid %s: the other side delivered", m.Txid))
	}
	now := i.now().UTC()
	if err := i.Memory.SetNote(ctx, AskWaitingKey(id), now.Format(time.RFC3339)); err != nil {
		return posternApplied{}, fmt.Errorf("restarting the wait on %s for the Governor: %w", id, err)
	}
	if name == PosternActionChase {
		return done(fmt.Sprintf("Chased: %s", id),
			fmt.Sprintf("CHASED by the Governor via postern, txid %s: the wait starts again from %s", m.Txid, now.Format(time.RFC3339)))
	}
	return done(fmt.Sprintf("Keep waiting: %s", id),
		fmt.Sprintf("KEEP WAITING by the Governor via postern, txid %s: no chase until %s", m.Txid,
			AddWorkingDays(now, ChaseAfterWorkingDays).Format("2006-01-02")))
}

// applyClose closes bead for the Governor: a story, ticket or hitl bead
// itself; an epic or map its held children first, at any depth, then itself,
// each with the Governor's reason. Nothing is closed when any child is
// in_progress, claimed or open and not held: the Mayor is told, naming it, and
// the bead is left without a comment (section 13). A bead already closed, or a
// story someone holds, is refused likewise.
func (i PosternInbox) applyClose(ctx context.Context, m PosternInboxMessage, bead StoryDetail,
	refuse func(string) (posternApplied, error), done func(subject, comment string) (posternApplied, error)) (posternApplied, error) {
	if bead.Closed() {
		return refuse("it is already closed")
	}
	closes := []string{bead.Story.ID}
	if bead.IsEpic {
		held, refusal, err := i.heldUnder(ctx, bead.Story.ID, map[string]bool{})
		if err != nil {
			return posternApplied{}, err
		}
		if refusal != "" {
			return refuse(refusal)
		}
		closes = append(held, closes...)
	} else if isClaimed(bead) {
		return refuse(fmt.Sprintf("it is %s%s", bead.Status, claimedBy(bead.Assignee)))
	}
	reason := fmt.Sprintf("Closed by the Governor via postern (%s)", m.Txid)
	// The comment first: the epic's own is the record of the close, and a
	// close that fails part-way leaves no bead the Governor's word is not on.
	subject := fmt.Sprintf("Closed: %s", bead.Story.ID)
	result, err := done(subject, reason)
	if err != nil {
		return posternApplied{}, err
	}
	for _, id := range closes {
		if err := i.Tracker.CloseStory(ctx, id, reason); err != nil {
			return refuse(fmt.Sprintf("closing %s failed: %v", id, err))
		}
	}
	// A card closed with a Then line files it as a card of its own, once: a
	// second close finds the card closed above and is refused.
	if _, err := fileThenCard(ctx, i.Tracker, bead); err != nil {
		return posternApplied{}, fmt.Errorf("closing %s for the Governor: %w", bead.Story.ID, err)
	}
	return result, nil
}

// heldUnder is the held (deferred) beads under epic, deepest first, or why
// the close is refused: the first bead under it is in_progress or claimed, or
// is open and not held — released work the Governor's close is not for, and
// which beads would refuse the epic's own close for.
func (i PosternInbox) heldUnder(ctx context.Context, epic string, seen map[string]bool) (held []string, refusal string, err error) {
	if seen[epic] {
		return nil, "", nil
	}
	seen[epic] = true
	detail, err := i.Tracker.ShowEpic(ctx, epic)
	if err != nil {
		return nil, "", fmt.Errorf("reading %s's children for the Governor's close: %w", epic, err)
	}
	for _, child := range detail.Stories {
		if child.Closed() {
			continue
		}
		if isClaimed(child) {
			return nil, child.Story.ID + " is being worked", nil
		}
		if !child.Held() {
			return nil, child.Story.ID + " is open: close or hold it first", nil
		}
		if child.IsEpic {
			below, why, err := i.heldUnder(ctx, child.Story.ID, seen)
			if err != nil || why != "" {
				return nil, why, err
			}
			held = append(held, below...)
		}
		held = append(held, child.Story.ID)
	}
	return held, "", nil
}

// isClaimed reports whether someone is working bead: it is in_progress, or is
// open with an assignee.
func isClaimed(bead StoryDetail) bool {
	return strings.EqualFold(strings.TrimSpace(bead.Status), StatusInProgress) || strings.TrimSpace(bead.Assignee) != ""
}

// claimedBy is ", claimed by <who>" for a story someone holds, "" otherwise.
func claimedBy(assignee string) string {
	if strings.TrimSpace(assignee) == "" {
		return ""
	}
	return ", claimed by " + assignee
}

// mail sends the Mayor one message from mw on this host. A nil Mailbox sends
// nothing.
func (i PosternInbox) mail(ctx context.Context, subject, body string) error {
	if i.Mailbox == nil {
		return nil
	}
	_, err := i.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, i.Host),
		To:      MayorMailbox,
		Subject: subject,
		Body:    body,
	})
	return err
}

// lock takes Lock, when there is one, so that two passes — the hook run
// twice at once, or the hook and the Mayor's own read — never apply the same
// message together; the release it reports is a no-op without one.
func (i PosternInbox) lock(ctx context.Context) (func(), error) {
	if i.Lock == nil {
		return func() {}, nil
	}
	release, err := i.Lock.Take(ctx)
	if err != nil {
		return nil, fmt.Errorf("taking the postern inbox's lock: %w", err)
	}
	return release, nil
}

// answerSuffix is the answer command a mail body about message m ends with,
// after a blank line; "" when there is none to give.
func answerSuffix(m PosternInboxMessage) string {
	if line := m.answerLine(); line != "" {
		return "\n\n" + line
	}
	return ""
}

// giveIdeasBack is the release the hook runs first: when Ideas only is not on
// and it holds ideas, it gives them back and says so in a line. A failure is
// said and gone on from, so that a record of his is never lost for it; the
// ideas stay held until the next pass or mw ideas-only release. It reports the
// taps applied.
func (i PosternInbox) giveIdeasBack(ctx context.Context) []string {
	if i.Ideas == nil || i.Ideas.Files == nil || i.Cards == nil {
		return nil
	}
	if held, err := i.Ideas.Files.Held(ctx); err != nil || len(held) == 0 {
		return nil
	}
	report, err := i.releaseIdeas(ctx)
	if err != nil {
		i.printf("mw postern inbox --apply: giving back the ideas of Ideas only: %v\n", err)
		return nil
	}
	if !report.Given() {
		return nil
	}
	i.printf("%s\n", strings.SplitN(report.String(), "\n", 2)[0])
	for _, line := range append(append([]string{}, report.Applied...), report.Failed...) {
		i.printf("%s\n", line)
	}
	return report.Applied
}

// isKeepAllTap and isDropAllTap report whether answer's word is "keep them all"
// or "drop them all": the two answers of the weekly Let go card.
func isKeepAllTap(answer string) bool {
	return posternAnswerWord(answer) == posternAnswerWord(TodoKeepAllOption)
}

func isDropAllTap(answer string) bool {
	return posternAnswerWord(answer) == posternAnswerWord(TodoDropAllOption)
}

// letGoOnTap answers the weekly Let go card of the list epic: keep puts every
// card the card listed back in later, and touches it with a comment; otherwise
// each is closed with the reason let-go. It acts only when m's verified sender is
// this host's GovernorKey, noteValue shows the question offered the answer, and
// the job kept the cards it listed (TodoLetGoKey): any other card that happens
// to offer the same words is not this. A card closed since, or no longer in
// let-go, is left as it is. The Mayor is mailed what was done, or why not.
func (i PosternInbox) letGoOnTap(ctx context.Context, m PosternInboxMessage, epic, noteValue string, keep bool) error {
	option, verb := TodoDropAllOption, "Dropped"
	if keep {
		option, verb = TodoKeepAllOption, "Kept"
	}
	listed, err := i.Memory.Note(ctx, TodoLetGoKey(epic))
	if err != nil {
		return fmt.Errorf("reading the cards of the Let go card of %s: %w", epic, err)
	}
	var ids []string
	if json.Unmarshal([]byte(listed), &ids) != nil || len(ids) == 0 {
		return nil
	}
	notApplied := func(why string) error {
		return i.mail(ctx, fmt.Sprintf("Let go not applied: %s", epic), fmt.Sprintf("%s: %q not applied, %s", epic, option, why))
	}
	if !i.isGovernor(m) {
		return notApplied("the tap's signer unchecked")
	}
	if !posternQuestionOffered(noteValue, posternAnswerWord(option)) {
		return notApplied("its question never offered it")
	}
	found, err := i.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		return fmt.Errorf("reading the cards of the Let go card of %s: %w", epic, err)
	}
	var done []string
	for _, card := range found {
		if why := refuseNotACard(card); why != "" || card.TodoRow != TodoRowLetGo {
			continue
		}
		id := card.Story.ID
		comment := fmt.Sprintf("%s by the Governor via postern, txid %s: on the weekly Let go card", strings.ToUpper(verb), m.Txid)
		if err := i.Tracker.CommentOnStory(ctx, id, comment); err != nil {
			return fmt.Errorf("recording the Governor's answer to the Let go card on %s: %w", id, err)
		}
		if keep {
			err = i.Tracker.SetStoryMetadata(ctx, id, map[string]string{
				TodoRowField: TodoRowLater, TodoRowWhyField: "Kept on the weekly Let go card",
			})
		} else {
			err = i.Tracker.CloseStory(ctx, id, TodoLetGoReason)
		}
		if err != nil {
			return notApplied(fmt.Sprintf("%s failed: %v", id, err))
		}
		done = append(done, id)
	}
	if err := i.Memory.ClearNote(ctx, TodoLetGoKey(epic)); err != nil {
		return fmt.Errorf("clearing the cards of the Let go card of %s: %w", epic, err)
	}
	return i.mail(ctx, fmt.Sprintf("%s: the Let go card of %s", verb, epic),
		fmt.Sprintf("%s by the Governor's tap (txid %s): %d cards: %s", verb, m.Txid, len(done), joinOrNone(done)))
}

// isDigestLooksGoodTap reports whether answer's word begins "looks good on
// all", or is "looks good": the first option of the weekly digest card
// (DigestLooksGoodOption), which with no demos is the bare words.
func isDigestLooksGoodTap(answer string) bool {
	word := posternAnswerWord(answer)
	return word == "looks good" || strings.HasPrefix(word, "looks good on all ")
}

// digestOnTap closes the batch on the Governor's "A: Looks good on all N" to
// the weekly digest bead: each demo the digest lists that is still open and
// unclaimed gets his tap quoted on it, as applyLooksGood writes it, and is
// closed; then the digest itself. It acts only on an open bead titled "Weekly
// digest", and only when m's verified sender is this host's GovernorKey and
// noteValue shows the question offered the answer; any other bead is not its
// and nothing is said. A listed demo already closed, claimed or gone is
// skipped and named in the mail. A comment or close that fails stops the
// batch, leaves the digest open and mails the Mayor "Not applied".
func (i PosternInbox) digestOnTap(ctx context.Context, m PosternInboxMessage, bead, answer, noteValue string) error {
	found, err := i.Tracker.ShowBeads(ctx, []string{bead})
	if err != nil {
		return fmt.Errorf("reading %s for the Governor's tap on the weekly digest: %w", bead, err)
	}
	if len(found) == 0 || found[0].IsEpic || found[0].Closed() || !isDigest(found[0]) {
		return nil
	}
	digest := found[0]
	notApplied := func(why string) error {
		return i.mail(ctx, fmt.Sprintf("Not applied: the weekly digest %s", bead),
			fmt.Sprintf("The Governor said %q by postern (txid %s) to the weekly digest %s; not applied: %s.", answer, m.Txid, bead, why))
	}
	if !i.isGovernor(m) {
		return notApplied("the tap's signer unchecked")
	}
	if !posternQuestionOffered(noteValue, posternAnswerWord(answer)) {
		return notApplied("its question never offered it")
	}
	listed := digestListed(digest.Description)
	demos := map[string]StoryDetail{}
	if len(listed) > 0 {
		shown, err := i.Tracker.ShowBeads(ctx, listed)
		if err != nil {
			return fmt.Errorf("reading the demos of the weekly digest %s: %w", bead, err)
		}
		for _, d := range shown {
			demos[d.Story.ID] = d
		}
	}
	var closed, skipped []string
	for _, id := range listed {
		demo, ok := demos[id]
		switch {
		case !ok:
			skipped = append(skipped, id+" (not found)")
			continue
		case demo.IsEpic || !isDemo(demo):
			skipped = append(skipped, id+" (not a demo)")
			continue
		case demo.Closed():
			skipped = append(skipped, id+" (already closed)")
			continue
		case isClaimed(demo):
			skipped = append(skipped, id+" (claimed)")
			continue
		}
		reason := fmt.Sprintf("Demo accepted on the Governor's Looks good via the weekly digest (txid %s)", m.Txid)
		comment := fmt.Sprintf("The Governor by postern %s, on the weekly digest %s: %q (txid %s). Demo accepted on the Governor's Looks good via the weekly digest.",
			sentInFull(m.Ts), bead, strings.TrimSpace(answer), m.Txid)
		// The comment first, as applyLooksGood writes it: a close that fails
		// leaves no bead the Governor's word is not on.
		if err := i.Tracker.CommentOnStory(ctx, id, comment); err != nil {
			return notApplied(fmt.Sprintf("commenting on %s failed: %v (closed: %s)", id, err, joinOrNone(closed)))
		}
		if err := i.Tracker.CloseStory(ctx, id, reason); err != nil {
			return notApplied(fmt.Sprintf("closing %s failed: %v (closed: %s)", id, err, joinOrNone(closed)))
		}
		closed = append(closed, id)
	}
	reason := fmt.Sprintf("Weekly digest taken on the Governor's Looks good (txid %s)", m.Txid)
	if err := i.Tracker.CloseStory(ctx, bead, reason); err != nil {
		return notApplied(fmt.Sprintf("closed %s, then closing the digest failed: %v", joinOrNone(closed), err))
	}
	return i.mail(ctx, fmt.Sprintf("Closed: the weekly digest %s on his Looks good", bead),
		fmt.Sprintf("The Governor said %q by postern (txid %s) to the weekly digest %s.\nClosed %d demos: %s.\nSkipped: %s.",
			strings.TrimSpace(answer), m.Txid, bead, len(closed), joinOrNone(closed), joinOrNone(skipped)))
}

// PosternTaps is the follower's applier of the Governor's one-tap actions
// (Release, Hold, Priority, Keep, Verified, Done and the rest of section 13):
// each pass of it applies, at zero tokens and through the same Apply path as
// mw postern inbox --apply, the taps that came in since, so a tap does not wait
// for a seat to run the inbox. His typed words, answers and voice notes are
// left as they are for the seat, and a tap seen twice, here or by the inbox, is
// applied once (the postern.applied note). It is held across the passes.
type PosternTaps struct {
	// Inbox is wired as mw postern inbox --apply wires it.
	Inbox PosternInbox
	// Every is the least time between two reads of the backend; zero reads on
	// every pass.
	Every time.Duration
	// Now is the clock Every is kept by; nil is time.Now.
	Now func() time.Time

	last  time.Time
	after int64
}

// Tap applies the taps that came in since the last pass, once Every has passed
// since it last read, and says what it did on the inbox's Out.
func (t *PosternTaps) Tap(ctx context.Context) error {
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	if t.Every > 0 && !t.last.IsZero() && now().Sub(t.last) < t.Every {
		return nil
	}
	t.last = now()
	_, resume, err := t.Inbox.apply(ctx, true, t.after)
	if err != nil {
		return err
	}
	t.after = resume
	return nil
}

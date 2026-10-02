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
	PosternActionVerified = "verified"
	PosternActionKeep     = "keep"
	PosternActionClose    = "close"
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
	if action.Action == "" || strings.TrimSpace(action.Bead) == "" {
		return PosternAction{}, false
	}
	return action, true
}

// knownPosternAction reports whether this host knows how to apply action. One
// it does not is left for the Mayor to read as text.
func knownPosternAction(action string) bool {
	switch action {
	case PosternActionRelease, PosternActionHold, PosternActionPriority, PosternActionVerified, PosternActionRun,
		PosternActionKeep, PosternActionClose:
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
func (i PosternInbox) Apply(ctx context.Context) ([]string, error) {
	if err := i.wired(); err != nil {
		return nil, err
	}
	i.prompts = &promptCache{}
	if i.Tracker == nil {
		return nil, fmt.Errorf("mw postern inbox --apply: no work tracker is configured to apply anything to")
	}
	release, err := i.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	home, err := i.homeHost(ctx)
	if err != nil {
		return nil, err
	}
	pass := moveHomePass{home: home}
	mine, applied, err := i.toApply(ctx, &pass)
	if err != nil {
		return nil, err
	}
	pass.applied = applied
	var lines []string
	for _, m := range mine {
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
		var outcome string
		var saved []posternSavedFile
		if len(m.files()) > 0 && (m.ThreadIsBead || i.isVoiceNote(m)) {
			outcome, saved = i.attachmentOutcome(ctx, m)
		}
		result, handled, err := i.applyOne(ctx, m, outcome, saved)
		if err != nil {
			return lines, err
		}
		if !handled {
			continue
		}
		if err := i.markApplied(ctx, applied, result); err != nil {
			return lines, err
		}
		lines = append(lines, result.summary())
		i.printf("%s\n", result.summary())
	}
	return lines, nil
}

// toApply reads what a pass may apply and the txids already applied. On the
// home a failure to read either is the pass's failure. On a boost, whose beads
// server is the home's and may be dead, each read is bounded by
// PosternBoostWait, and one that fails is said and set aside rather than
// stopping the pass: every record is read from the start, none is taken for
// applied, and the pass is dark (a move-home then writes nothing before its
// move), so that a move the Governor asked for is never held up by the host
// it moves away from.
func (i PosternInbox) toApply(ctx context.Context, pass *moveHomePass) ([]PosternInboxMessage, map[string]string, error) {
	if !pass.onBoost(i.Host) {
		mine, _, _, err := i.fetch(ctx)
		if err != nil {
			return nil, nil, err
		}
		applied, err := i.appliedNotes(ctx)
		return mine, applied, err
	}
	bounded, cancel := i.bounded(ctx, *pass)
	cursor, err := i.readCursor(bounded)
	cancel()
	if err != nil {
		i.printf("this host is not home and the postern inbox cursor could not be read (%v): every record is read for a move-home\n", err)
		cursor, pass.dark = 0, true
	}
	mine, _, err := i.fetchSince(ctx, cursor)
	if err != nil {
		return nil, nil, err
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
	return mine, applied, nil
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
			fmt.Sprintf("The Governor by postern %s, txid %s, on %s:\n\n%s%s", sentInFull(m.Ts), m.Txid, m.Thread, m.Text, answerSuffix(m))); err != nil {
			return posternApplied{}, false, err
		}
		result := posternApplied{Kind: "comment", Bead: m.Thread, Txid: m.Txid}
		if len(saved) > 0 {
			result.Detail = posternSavedNames(saved)
		}
		return result, true, nil
	}
	return posternApplied{}, false, nil
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

// applyAction applies one of the Governor's section 13 actions to its bead,
// as the Governor, at zero tokens: release, hold, priority, verified, keep or
// close. Each done is commented on its bead and mailed to the Mayor; each that
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
				"HELD by the Governor via postern, txid %s: the story is claimed%s, so its session is being cancelled (control event %d)",
				m.Txid, claimedBy(bead.Assignee), cancel.Seq))
		}
		if !strings.EqualFold(strings.TrimSpace(bead.Status), StatusOpen) || strings.TrimSpace(bead.Assignee) != "" {
			return refuse(fmt.Sprintf("it is %s%s, not open and unclaimed", bead.Status, claimedBy(bead.Assignee)))
		}
		if err := i.Tracker.HoldStory(ctx, action.Bead); err != nil {
			return refuse(err.Error())
		}
		return done(fmt.Sprintf("Held: %s", action.Bead), fmt.Sprintf("HELD by the Governor via postern, txid %s", m.Txid))

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

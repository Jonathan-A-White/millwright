package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
)

// PosternAction is an action's plaintext, section 13: a JSON object naming
// what to do and the bead to do it to, and — for a priority — the priority.
type PosternAction struct {
	Action   string `json:"action"`
	Bead     string `json:"bead"`
	Priority *int   `json:"priority,omitempty"`
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
	case PosternActionRelease, PosternActionHold, PosternActionPriority, PosternActionVerified:
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
}

// summary is the line a pass prints for it: never a message's text.
func (a posternApplied) summary() string {
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
func (i PosternInbox) Apply(ctx context.Context) ([]string, error) {
	if err := i.wired(); err != nil {
		return nil, err
	}
	if i.Tracker == nil {
		return nil, fmt.Errorf("mw postern inbox --apply: no work tracker is configured to apply anything to")
	}
	release, err := i.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	mine, _, _, err := i.fetch(ctx)
	if err != nil {
		return nil, err
	}
	applied, err := i.appliedNotes(ctx)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, m := range mine {
		if m.Txid == "" || !m.Verified || !i.isGovernor(m) {
			continue
		}
		if _, done := applied[m.Txid]; done {
			continue
		}
		var outcome, path string
		if m.Attachment != nil && (m.ThreadIsBead || i.isVoiceNote(m)) {
			outcome, path = i.attachmentOutcome(ctx, m)
		}
		result, handled, err := i.applyOne(ctx, m, outcome, path)
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
// did and whether it was one to apply at all. outcome and path are its
// attachment's download, when it has one and was downloaded: the line a
// reader is shown, and the file written.
func (i PosternInbox) applyOne(ctx context.Context, m PosternInboxMessage, outcome, path string) (posternApplied, bool, error) {
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
		if !knownPosternAction(action.Action) || !m.SignerChecked {
			return posternApplied{}, false, nil
		}
		result, err := i.applyAction(ctx, m, action)
		return result, err == nil, err
	}
	if i.isVoiceNote(m) {
		result, err := i.applyVoice(ctx, m, outcome, path)
		return result, err == nil, err
	}
	if m.ThreadIsBead {
		recorded, fresh, err := i.recordThreadCommentOnce(ctx, m, path)
		if err != nil || !recorded {
			return posternApplied{}, false, err
		}
		if !fresh {
			return posternApplied{Kind: "comment", Bead: m.Thread, Txid: m.Txid}, true, nil
		}
		if err := i.mail(ctx, fmt.Sprintf("Governor on %s: %s", m.Thread, clippedTo(strings.Join(strings.Fields(m.Text), " "), 60)),
			fmt.Sprintf("The Governor by postern %s, txid %s, on %s:\n\n%s", sentInFull(m.Ts), m.Txid, m.Thread, m.Text)); err != nil {
			return posternApplied{}, false, err
		}
		result := posternApplied{Kind: "comment", Bead: m.Thread, Txid: m.Txid}
		if path != "" {
			result.Detail = fmt.Sprintf("[%s: %s]", posternAttachmentLabel(m.Attachment.Mime), path)
		}
		return result, true, nil
	}
	return posternApplied{}, false, nil
}

// applyAction applies one of the Governor's section 13 actions to its bead,
// as the Governor, at zero tokens: release, hold, priority or verified. Each
// done is commented on its bead and mailed to the Mayor; each that cannot be
// done — no such bead, a hold on a story already claimed, a priority out of
// range — is refused, and the Mayor is mailed why. Either way it is applied,
// so it is never tried again.
func (i PosternInbox) applyAction(ctx context.Context, m PosternInboxMessage, action PosternAction) (posternApplied, error) {
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

	default: // PosternActionVerified
		return done(fmt.Sprintf("Verified: %s", action.Bead), fmt.Sprintf("VERIFIED by the Governor via postern (%s)", m.Txid))
	}
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

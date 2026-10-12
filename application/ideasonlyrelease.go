package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The end of Ideas only (postern's docs/protocol.md section 25): mw gives the
// Governor's ideas back, once. One live card, 'Your ideas from Ideas only (N)',
// goes to him with an item for each idea; the taps he made while the mode was
// on are applied in the order he made them; the ideas are unread again in the
// inbox, so that the Mayor handles each as a normal message; and the held file
// is put away under released/. mw talk wait, the inbox's hook and mw ideas-only
// release each do it, whichever sees the end first.

// IdeasOnlyCardTitle is the title of the card that gives n ideas back.
func IdeasOnlyCardTitle(n int) string {
	return fmt.Sprintf("Your ideas from Ideas only (%d)", n)
}

// IdeasOnlyRelease is mw ideas-only release: it gives the held ideas back when
// the mode is not on, and says so when it is. Inbox is the inbox as the hook
// wires it (the tracker the taps are applied through, the mailbox, the sender)
// with Cards set to the card sender and Ideas to the mode.
type IdeasOnlyRelease struct {
	Inbox PosternInbox
}

// IdeasOnlyReleased is what a release found and did.
type IdeasOnlyReleased struct {
	// StillOn says the mode is on, so nothing was given back; State is its note.
	StillOn bool
	State   IdeasOnlyState
	// Ended is when the mode ended; Now is when the release ran.
	Ended time.Time
	Now   time.Time
	// Ideas are the held ideas given back, oldest first; Card is the txid of the
	// card that gave them.
	Ideas []IdeasOnlyHeld
	Card  string
	// Applied is a line for each held tap applied; Failed one for each that
	// could not be, which is left for the inbox's own read to try again.
	Applied []string
	Failed  []string
}

// Given reports whether any ideas were given back.
func (r IdeasOnlyReleased) Given() bool { return !r.StillOn && len(r.Ideas) > 0 }

// String is the report as it is printed: 'Ideas only ended 21:00: 3 ideas' and a
// line for each, or 'Ideas only ended: nothing held', or that it is still on.
func (r IdeasOnlyReleased) String() string {
	switch {
	case r.StillOn:
		return fmt.Sprintf("Ideas only is still on until %s: nothing released\n", IdeasOnlyUntilLabel(r.State, r.Now))
	case len(r.Ideas) == 0:
		return "Ideas only ended: nothing held\n"
	}
	var b strings.Builder
	n := len(r.Ideas)
	noun := "ideas"
	if n == 1 {
		noun = "idea"
	}
	fmt.Fprintf(&b, "Ideas only ended %s: %d %s\n", clockLabel(r.Ended, time.UTC, r.Now), n, noun)
	for _, held := range r.Ideas {
		fmt.Fprintf(&b, "  %s  %s  %s  %s  %s\n", held.Txid, held.Channel, held.Class, held.When, held.Text)
	}
	if r.Card != "" {
		fmt.Fprintf(&b, "card %s\n", r.Card)
	}
	for _, line := range r.Applied {
		fmt.Fprintf(&b, "%s\n", line)
	}
	for _, line := range r.Failed {
		fmt.Fprintf(&b, "%s\n", line)
	}
	return b.String()
}

// Run gives the ideas back under the inbox's lock, so that it never runs with
// the hook or the wait keeping a record.
func (r IdeasOnlyRelease) Run(ctx context.Context) (IdeasOnlyReleased, error) {
	switch {
	case r.Inbox.Ideas == nil || r.Inbox.Ideas.Files == nil:
		return IdeasOnlyReleased{}, fmt.Errorf("mw ideas-only release: nowhere to read the state of Ideas only from")
	case r.Inbox.Cards == nil:
		return IdeasOnlyReleased{}, fmt.Errorf("mw ideas-only release: no card sender is configured")
	}
	if err := r.Inbox.wired(); err != nil {
		return IdeasOnlyReleased{}, err
	}
	release, err := r.Inbox.lock(ctx)
	if err != nil {
		return IdeasOnlyReleased{}, err
	}
	defer release()
	return r.Inbox.releaseIdeas(ctx)
}

// releaseIdeas is the release, with the inbox's lock already held. A mode that
// is on gives nothing back; nothing held is said and sends no card.
func (i PosternInbox) releaseIdeas(ctx context.Context) (IdeasOnlyReleased, error) {
	i.prompts = &promptCache{}
	state, err := i.Ideas.Refresh(ctx)
	if err != nil {
		i.printf("%v; going on with the last note of Ideas only\n", err)
	}
	now := i.Ideas.now()
	if state.OnAt(now) {
		return IdeasOnlyReleased{StillOn: true, State: state, Now: now}, nil
	}
	held, err := i.Ideas.Files.Held(ctx)
	if err != nil {
		return IdeasOnlyReleased{}, err
	}
	report := IdeasOnlyReleased{State: state, Ended: state.EndedAt(now), Now: now, Ideas: held}
	if len(held) == 0 {
		return report, nil
	}

	// The card first, once: a release that dies after it leaves its txid in the
	// state file, and the next one goes on from there without sending another.
	report.Card = state.Releasing
	if report.Card == "" {
		_, txid, err := i.Cards.Send(ctx, CardSendRequest{
			Title: IdeasOnlyCardTitle(len(held)), Items: ideaCardItems(held, report.Ended),
		})
		if err != nil {
			return report, fmt.Errorf("giving back the ideas of Ideas only: %w", err)
		}
		report.Card, state.Releasing = txid, txid
		if err := i.Ideas.Files.SetState(ctx, state); err != nil {
			return report, err
		}
	}

	report.Applied, report.Failed = i.applyHeldTaps(ctx, held)

	for _, idea := range held {
		if err := i.Memory.ClearNote(ctx, PosternHeldKey(idea.Txid)); err != nil {
			return report, fmt.Errorf("letting go of the mark on %s: %w", idea.Txid, err)
		}
	}
	if err := i.unreadFrom(ctx, held); err != nil {
		return report, err
	}
	if err := i.Ideas.Files.Release(ctx, report.Ended); err != nil {
		return report, err
	}
	state, err = i.Ideas.Files.State(ctx)
	if err != nil {
		return report, err
	}
	state.ReleasedUntil, state.Releasing = report.Ended.Unix(), ""
	return report, i.Ideas.Files.SetState(ctx, state)
}

// ideaCardItems is one item for each idea, in the order they were kept: its
// first 80 runes, then where he sent it and when, '(general, 11:58 UTC)'. An
// item carries no link and expects nothing: the Mayor adds links with mw card
// update as he files each idea.
func ideaCardItems(held []IdeasOnlyHeld, ended time.Time) []string {
	items := make([]string, 0, len(held))
	for n, idea := range held {
		when := idea.When
		if at, err := time.Parse(time.RFC3339, idea.When); err == nil {
			when = clockLabel(at, time.UTC, ended) + " UTC"
		}
		text := strings.NewReplacer("|", "/", "\n", " ").Replace(idea.Text)
		items = append(items, fmt.Sprintf("%d. %s (%s, %s)", n+1, text, idea.Channel, when))
	}
	return items
}

// applyHeldTaps applies, in the order he made them, each of the held records
// that is one of his section 13 taps and not yet applied: as the hook would
// have, through applyOne. It reports a line for each applied and for each that
// could not be, so that one bad tap never holds the rest back; the inbox's own
// read tries it again. With no tracker nothing can be applied, and the taps
// are left to that read.
func (i PosternInbox) applyHeldTaps(ctx context.Context, held []IdeasOnlyHeld) (applied, failed []string) {
	if i.Tracker == nil {
		return nil, nil
	}
	earliest, ok := earliestInboxSeq(held)
	if !ok {
		return nil, nil
	}
	heldTxids := make(map[string]bool, len(held))
	for _, idea := range held {
		heldTxids[idea.Txid] = true
	}
	mine, _, err := i.fetchSince(ctx, earliest-1)
	if err != nil {
		return nil, []string{fmt.Sprintf("the held taps were not applied, the records could not be read: %v", err)}
	}
	done, err := i.appliedNotes(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("the held taps were not applied: %v", err)}
	}
	for _, m := range mine {
		if !heldTxids[m.Txid] || !m.Verified || !i.isGovernor(m) || appliedTxid(done, m.Txid) {
			continue
		}
		if _, isTap := decodePosternAction(m.Text); !isTap {
			continue
		}
		result, handled, err := i.applyOne(ctx, m, "", nil)
		if err == nil && handled {
			err = i.markApplied(ctx, done, result)
		}
		switch {
		case err != nil:
			failed = append(failed, fmt.Sprintf("not applied: %s: %v", m.Txid, err))
		case handled:
			applied = append(applied, result.summary())
		}
	}
	return applied, failed
}

// earliestInboxSeq is the lowest sequence number among the held records the
// inbox reads: a talk turn or a call record is mw talk wait's, never its.
func earliestInboxSeq(held []IdeasOnlyHeld) (int64, bool) {
	var earliest int64
	found := false
	for _, idea := range held {
		if idea.Class == talkRecordClass || idea.Class == callRecordClass || idea.Seq <= 0 {
			continue
		}
		if !found || idea.Seq < earliest {
			earliest, found = idea.Seq, true
		}
	}
	return earliest, found
}

// unreadFrom puts the inbox's cursor back to just before the earliest held
// record it reads, when it had passed it, so that mw postern inbox shows each
// held idea as unread.
func (i PosternInbox) unreadFrom(ctx context.Context, held []IdeasOnlyHeld) error {
	earliest, ok := earliestInboxSeq(held)
	if !ok {
		return nil
	}
	cursor, err := i.readCursor(ctx)
	if err != nil {
		return err
	}
	if cursor < earliest {
		return nil
	}
	return i.Memory.SetNote(ctx, PosternCursorKey, fmt.Sprint(earliest-1))
}

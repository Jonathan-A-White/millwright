package application

import (
	"context"
	"fmt"
	"strings"
)

// stuckKey is the note that remembers, per host and story, the last error a
// dispatch failed to start it with: "seen|error" for a first sighting, or
// "told|error" once the Mayor was told of it.
func stuckKey(host, id string) string { return "dispatch.stuck." + host + "." + id }

const (
	stuckSeen = "seen"
	stuckTold = "told"
)

// tellStuck mails the Mayor when this host's dispatch fails to start the same
// story with the same error twice running (mw-gq6.259): every tick claims the
// story and gives it back, and nobody reads the timer's log. The first failure
// only marks it seen; the second identical one sends one mail, and that error
// on that story and host is not mailed again. A different error is a new
// reason and waits for its own second sighting.
//
// The error is compared by its first line. With no Mailbox, or no Memory to
// keep the sighting in, nothing is sent: mail on every tick would be worse than
// none. A mail or note that fails is a note on the report, and the next tick
// tries again.
func (d Dispatch) tellStuck(ctx context.Context, id string, failure error, report *DispatchReport) {
	if d.Mailbox == nil || d.Memory == nil {
		return
	}
	first := stuckReason(failure)
	key := stuckKey(d.Host, id)
	last, _ := d.Memory.Note(ctx, key)
	state, was, _ := strings.Cut(last, "|")
	if last == "" || was != first {
		if err := d.Memory.SetNote(ctx, key, stuckSeen+"|"+first); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: it failed to start, but that could not be remembered: %v", id, err))
		}
		return
	}
	if state == stuckTold {
		return
	}
	if _, err := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("Stuck: %s on %s: %s", id, d.Host, first),
		Body: fmt.Sprintf("mw dispatch on %s has claimed %s and failed to start it, and given the claim back, twice running "+
			"with the same error:\n\n  %s\n\nEvery tick will do the same until the cause is dealt with. "+
			"This is mailed once for this story, host and error; a different error is mailed again.\n", d.Host, id, first),
	}); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the stuck mail to %s could not be sent: %v", id, MayorMailbox, err))
		return
	}
	if err := d.Memory.SetNote(ctx, key, stuckTold+"|"+first); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the stuck mail was sent, but that could not be remembered: %v", id, err))
	}
}

// stuckReason is the first line of a failure with the notes start adds after
// it cut off: "(the formula was already poured ...)" is there from the second
// tick on and not the first, and "(and the worktree ... is still there ...)"
// says what could not be undone, neither of which is what the story failed on.
func stuckReason(failure error) string {
	first, _, _ := strings.Cut(failure.Error(), "\n")
	for _, note := range []string{" (the ", " (and "} {
		first, _, _ = strings.Cut(first, note)
	}
	return clippedTo(strings.TrimSpace(first), DispatchLogReasonLimit)
}

// clearStuck forgets what a story last failed to start with once it has
// started, so that it failing again is a first sighting again.
func (d Dispatch) clearStuck(ctx context.Context, id string) {
	if d.Memory == nil {
		return
	}
	key := stuckKey(d.Host, id)
	if last, _ := d.Memory.Note(ctx, key); last != "" {
		_ = d.Memory.ClearNote(ctx, key)
	}
}

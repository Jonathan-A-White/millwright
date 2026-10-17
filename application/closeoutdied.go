package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// CloseOutDiedSubject is the subject of the mail mw dispatch sends the Mayor
// when it reclaims a story whose session finished but whose close-out died
// before it recorded anything (mw-gq6.487).
func CloseOutDiedSubject(id string) string { return "Close-out died: " + id }

// CloseOutLogTail is how many of the close-out log's last lines that mail and
// the comment on the story carry.
const CloseOutLogTail = 20

// HeartbeatLogTail is how many of the heartbeat log's last lines the same
// mail and comment carry, when the log exists (mw-gq6.494).
const HeartbeatLogTail = 5

// CauseTooling is the class mw cause names an attempt that was not the story's
// own but the factory's: here, a close-out that died with the work finished.
const CauseTooling = "tooling"

// closeOutDied reports whether a dead pane is a close-out that died, not a
// session that did (mw-gq6.487): the session ran to its end and wrote its
// result for the story's current attempt, and no line of the seat's ledger
// charges that session, so the close-out chained after it never got as far as
// its one sure record. The run state and the unrecorded-outcome file were read
// by the caller already; this is what is left when both say nothing. Anything
// that cannot be read says no, and the story is reclaimed as it was before.
func (d Dispatch) closeOutDied(ctx context.Context, detail StoryDetail) (sessionID string, died bool) {
	if d.Boot.Vault == nil || d.Boot.Seat == "" {
		return "", false
	}
	id := detail.Story.ID
	printed, err := d.Boot.Vault.ReadRunFile(ctx, id, ResultFileNameForAttempt(max(1, detail.Attempts)))
	if err != nil {
		return "", false
	}
	result, err := ReadSessionResult(printed)
	if err != nil || result.SessionID == "" {
		return "", false
	}
	lines, err := d.Boot.Vault.ReadLedger(ctx, d.Boot.Seat)
	if err != nil {
		return "", false
	}
	for _, line := range lines {
		if LedgerChargesSession(line, result.SessionID) {
			return "", false
		}
	}
	return result.SessionID, true
}

// reclaimDiedCloseOut is reclaimDeadPane's ending for a close-out that died:
// the claim is already given back. Nothing is refunded, since the session's
// result is the evidence and a refund's next attempt would write over it;
// instead the attempt is named tooling (mw cause), so that it is not the
// story's own, the story is told what happened with the close-out's last
// words from its run, and the Mayor is mailed the same. A part that cannot be
// written is a note; the reclaim stands.
func (d Dispatch) reclaimDiedCloseOut(ctx context.Context, detail StoryDetail, session string, status SessionStatus, sessionID string, report *DispatchReport) bool {
	id := detail.Story.ID
	attempt := max(1, detail.Attempts)
	logName := NextLogNameForAttempt(attempt)
	tail := d.closeOutLogTail(ctx, id, logName)
	why := fmt.Sprintf("the session of attempt %d (%s) finished and left %s, but the close-out chained after it recorded nothing — "+
		"no ledger line, no comment, no mail, no close — so it died before it could", attempt, sessionID, ResultFileNameForAttempt(attempt))

	causes := map[int]string{}
	for at, class := range detail.AttemptCauses {
		causes[at] = class
	}
	causes[attempt] = CauseTooling
	fields := map[string]string{
		CauseField:         CauseTooling,
		CauseWhyField:      fmt.Sprintf("the close-out of attempt %d died before it recorded anything (mw dispatch on %s found its pane dead; its last words are in runs/%s/%s)", attempt, d.Host, id, logName),
		AttemptCausesField: attemptCausesText(causes),
	}
	named := fmt.Sprintf("attempt %d is named %s (mw cause), not the story's own, and was not refunded: its result stays as evidence", attempt, CauseTooling)
	if err := d.Tracker.SetStoryMetadata(ctx, id, fields); err != nil {
		named = fmt.Sprintf("attempt %d could not be named %s (mw cause): %v; it was not refunded", attempt, CauseTooling, err)
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its died close-out's attempt could not be named %s: %v", id, CauseTooling, err))
	}

	lastWords := fmt.Sprintf("The close-out's last words (runs/%s/%s):\n%s", id, logName, tail)
	lastWords += d.heartbeatLogTail(ctx, id, HeartbeatLogNameForAttempt(attempt))
	comment := fmt.Sprintf("mw dispatch on %s found %s claimed here with a dead pane (%s is %s) and its lease had run out, and its close-out died: %s. "+
		"The window was closed and the claim given back so it is dispatched again, its worktree and branch left for the next attempt; %s.\n\n%s",
		d.Host, id, session, status.State, why, named, lastWords)

	if err := d.Runner.Close(ctx, session); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf(
			"%s: its close-out died and the claim was given back, but the window %s could not be closed: %v", id, session, err))
	}
	if err := d.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the died-close-out reclaim could not be commented on: %v", id, err))
	}
	d.mailCloseOutDied(ctx, detail, why, named, lastWords, report)
	report.Reclaimed = append(report.Reclaimed, Reclaimed{StoryID: id, Session: session, LeaseExpired: detail.LeaseExpires, CloseOutDied: true})
	return true
}

// mailCloseOutDied tells the Mayor, with the same words the story was told.
func (d Dispatch) mailCloseOutDied(ctx context.Context, detail StoryDetail, why, named, lastWords string, report *DispatchReport) {
	id := detail.Story.ID
	if d.Mailbox == nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its close-out died, but this mw dispatch has no mailbox to tell %s", id, MayorMailbox))
		return
	}
	title := strings.Join(strings.Fields(detail.Story.Title), " ")
	if title == "" {
		title = id
	}
	body := fmt.Sprintf("%s (%s) on %s: %s.\n\nThe claim was given back and the story is dispatched again on the branch this attempt left; %s. "+
		"Last words that name a fault of the host (the beads server unreachable, a bd i/o timeout) need nothing: the next attempt's close-out lands the same work. "+
		"Anything else is mw's own and wants a bug filed.\n\n%s\n",
		id, title, d.Host, why, named, lastWords)
	if _, err := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: CloseOutDiedSubject(id),
		Body:    body,
	}); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the mail to %s that its close-out died could not be sent: %v", id, MayorMailbox, err))
	}
}

// closeOutLogTail is the last CloseOutLogTail lines of the close-out's log in
// the story's run, or one line saying why there are none.
func (d Dispatch) closeOutLogTail(ctx context.Context, id, logName string) string {
	kept, err := d.Boot.Vault.ReadRunFile(ctx, id, logName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("(none: no %s was kept, so the close-out's pane was the only place its words were)", logName)
	case err != nil:
		return fmt.Sprintf("(%s could not be read: %v)", logName, err)
	}
	lines := strings.Split(strings.TrimRight(kept, "\n"), "\n")
	if len(lines) > CloseOutLogTail {
		lines = lines[len(lines)-CloseOutLogTail:]
	}
	return strings.Join(lines, "\n")
}

// heartbeatLogTail is the last HeartbeatLogTail lines of the heartbeat's log in
// the story's run, set under a heading of their own, or nothing when no log was
// kept or it holds nothing: a lease that lapsed is the reason to read it, and a
// session whose heartbeat never failed has no words to quote (mw-gq6.494).
func (d Dispatch) heartbeatLogTail(ctx context.Context, id, logName string) string {
	kept, err := d.Boot.Vault.ReadRunFile(ctx, id, logName)
	if err != nil || strings.TrimSpace(kept) == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(kept, "\n"), "\n")
	if len(lines) > HeartbeatLogTail {
		lines = lines[len(lines)-HeartbeatLogTail:]
	}
	return fmt.Sprintf("\n\nThe heartbeat's last words (runs/%s/%s):\n%s", id, logName, strings.Join(lines, "\n"))
}

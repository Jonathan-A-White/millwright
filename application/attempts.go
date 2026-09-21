package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// AttemptsField is the metadata a story carries to say how many times a session
// was started for it: every dispatch that got as far as a running session, and
// the rebase session mw next sends a conflicted story back to. A dispatch that
// fails before its session starts is not an attempt.
//
// AttemptsExhaustedField is the marker mw dispatch leaves once it has said a
// story used up its attempts, holding the count it said it at. It is what keeps
// that to once: a tick that finds it on a story says nothing again. It is
// cleared, on the story's next attempt, so that a story reset by hand and used
// up again is said again.
const (
	AttemptsField          = "attempts"
	AttemptsExhaustedField = "attempts_exhausted"
)

// DefaultMaxAttempts is how many times a story is tried when the config file
// says nothing: infrastructure/config.DefaultMaxAttempts is the same number.
const DefaultMaxAttempts = 3

// ReasonAttemptsExhausted is the code a story mw dispatch will not start again
// is marked blocked under: it has been tried as many times as a story may be.
const ReasonAttemptsExhausted Reason = "attempts-exhausted"

// RefusedPhrase is what a close-out's comment on a story says between "mw next
// on <host>" and the reason code it was refused for, so that the refusals
// recorded on a story can be read back out of its comments.
const RefusedPhrase = "did not close this story out ("

// maxAttempts is how many times a story may be started.
func (d Dispatch) maxAttempts() int {
	if d.MaxAttempts < 1 {
		return DefaultMaxAttempts
	}
	return d.MaxAttempts
}

// attemptFields is what a story is given when its attempt-th session has
// started: the count, and, when a tick had said the story used up its attempts,
// that mark cleared, because it is being tried again.
func attemptFields(detail StoryDetail, attempt int) map[string]string {
	fields := map[string]string{AttemptsField: strconv.Itoa(attempt)}
	if detail.Exhausted {
		fields[AttemptsExhaustedField] = ""
	}
	return fields
}

// recordAttempt counts a session started for a story that was not started by a
// dispatch — the rebase session mw next sends a conflicted story back to. It
// reports what could not be written.
func recordAttempt(ctx context.Context, tracker WorkTracker, detail StoryDetail) error {
	next := detail.Attempts + 1
	if err := tracker.SetStoryMetadata(ctx, detail.Story.ID, attemptFields(detail, next)); err != nil {
		return fmt.Errorf("the attempt could not be recorded as %s=%d: %w", AttemptsField, next, err)
	}
	return nil
}

// exhausted tells the Mayor a story has been started as many times as a story
// may be, once. The story is left unclaimed, exactly as it was found, and marked
// run=blocked under the code ReasonAttemptsExhausted, with one comment and one
// mail. A story already marked (AttemptsExhaustedField) has been told already, so
// a tick that finds it says nothing.
//
// The mark is written first and is what makes it once: a mark that cannot be
// written means nothing is said, and the next tick tries again; what is said
// after it and cannot be is a note on the report, and is not tried again.
func (d Dispatch) exhausted(ctx context.Context, detail StoryDetail, report *DispatchReport) {
	if detail.Exhausted {
		return
	}
	id, tried, most := detail.Story.ID, detail.Attempts, d.maxAttempts()
	unsaid := func(what string, err error) {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: %s: %v", id, what, err))
	}

	if err := d.Tracker.SetStoryMetadata(ctx, id, map[string]string{AttemptsExhaustedField: strconv.Itoa(tried)}); err != nil {
		unsaid("it used up its attempts, and that could not be recorded, so nobody was told", err)
		return
	}

	var refusals []string
	comments, err := d.Tracker.StoryComments(ctx, id)
	if err != nil {
		unsaid("the refusals recorded on it could not be read, so the mail leaves them out", err)
	}
	for _, comment := range comments {
		if refusal, ok := refusalIn(comment.Text); ok {
			refusals = append(refusals, refusal)
		}
	}

	why := fmt.Sprintf("it has been started %d times, the most a story may be (max_attempts is %d)", tried, most)
	comment := fmt.Sprintf("mw dispatch on %s did not start this story (%s): %s. It is left unclaimed and blocked, "+
		"and the Mayor has been told. It is started again only once its counter is reset by hand: "+
		"bd update %s --set-metadata %s=0", d.Host, ReasonAttemptsExhausted, why, id, AttemptsField)
	if err := d.Tracker.CommentOnStory(ctx, id, comment); err != nil {
		unsaid("the comment could not be written on it", err)
	}
	if err := d.Tracker.SetStoryState(ctx, id, RunState, RunBlocked, "("+string(ReasonAttemptsExhausted)+") "+why); err != nil {
		unsaid(fmt.Sprintf("it could not be recorded as %s=%s", RunState, RunBlocked), err)
	}
	if d.Mailbox == nil {
		return
	}

	title := strings.Join(strings.Fields(detail.Story.Title), " ")
	if title == "" {
		title = id
	}
	body := fmt.Sprintf("%s (%s) has been started %d times, the most a story may be (max_attempts is %d on %s). "+
		"mw dispatch did not start it again: it is unclaimed and marked %s=%s (%s).\n\n",
		id, title, tried, most, d.Host, RunState, RunBlocked, ReasonAttemptsExhausted)
	if len(refusals) == 0 {
		body += "No refusal is recorded on the story.\n"
	} else {
		body += "The refusals recorded on the story, oldest first:\n"
		for _, refusal := range refusals {
			body += "  - " + refusal + "\n"
		}
	}
	body += fmt.Sprintf("\nTo have it tried again, once the cause is dealt with: bd update %s --set-metadata %s=0\n", id, AttemptsField)
	if _, err := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: MailBlocked + ": " + title,
		Body:    body,
	}); err != nil {
		unsaid("the mail to "+MayorMailbox+" could not be sent", err)
	}
}

// refusalIn reads the refusal a comment records — the reason code and the first
// line of why, as mw next wrote them — or reports that the comment is not one.
func refusalIn(comment string) (string, bool) {
	line, _, _ := strings.Cut(comment, "\n")
	at := strings.Index(line, RefusedPhrase)
	if at < 0 {
		return "", false
	}
	return clippedTo(line[at+len(RefusedPhrase)-1:], DispatchLogReasonLimit), true
}

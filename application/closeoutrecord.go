package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// UnrecordedFileName is the file in a story's run that a close-out leaves when
// it stopped a story and could not write all of it down: the comment on the
// story, the run=blocked state, the mail to the Mayor. It holds the outcome the
// close-out meant to record, so that mw dispatch, finding the pane dead, writes
// it down instead of taking the story for a session that died and refunding its
// attempt (mw-gq6.375).
const UnrecordedFileName = "close-out-unrecorded.json"

// JobCloseOutUnrecorded is the actor of the job event a close-out writes when
// what it meant to record could not be, kept apart from the other jobs.
const JobCloseOutUnrecorded = "close-out"

// The parts of a stopped close-out's record that can be left unwritten.
const (
	UnrecordedComment  = "comment"
	UnrecordedRunState = "run-state"
	UnrecordedMail     = "mail"
)

// UnrecordedOutcome is what a close-out meant to record of a stopped story and
// could not, with the parts still to write in Pending.
type UnrecordedOutcome struct {
	Story   string    `json:"story"`
	Host    string    `json:"host"`
	At      time.Time `json:"at"`
	Outcome string    `json:"outcome"`
	Reason  Reason    `json:"reason"`
	Why     string    `json:"why"`
	// Attempt is the attempt the close-out was closing, so that a file left by
	// an earlier attempt is never replayed over a later one.
	Attempt     int      `json:"attempt"`
	Comment     string   `json:"comment"`
	StateDetail string   `json:"state_detail"`
	MailFrom    string   `json:"mail_from"`
	MailTo      string   `json:"mail_to"`
	MailSubject string   `json:"mail_subject"`
	MailBody    string   `json:"mail_body"`
	Pending     []string `json:"pending"`
}

func (u UnrecordedOutcome) pending(part string) bool {
	for _, p := range u.Pending {
		if p == part {
			return true
		}
	}
	return false
}

// keepUnrecorded writes what a stopped close-out could not record to the run,
// says so in the events log, and returns the notes for what could not be kept
// either. A failure that is nowhere but a pane's stderr is lost when the pane
// exits.
func (n Next) keepUnrecorded(ctx context.Context, c *closeOut, report *NextReport, out UnrecordedOutcome) []string {
	var trouble []string
	text, err := json.MarshalIndent(out, "", "  ")
	if err == nil {
		_, err = n.Vault.PutRunFile(ctx, c.id, UnrecordedFileName, string(text)+"\n")
	}
	if err != nil {
		trouble = append(trouble, fmt.Sprintf("what the close-out could not record of %s could not be kept in its run: %v", c.id, err))
	} else {
		c.unrecorded = true
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be written to %s (%s): the outcome is kept at %s for mw dispatch to replay",
			c.id, strings.Join(out.Pending, ", "), out.Reason, n.Vault.RunFile(c.id, UnrecordedFileName)))
	}
	n.saidInEvents(ctx, report, c, fmt.Sprintf("%s stopped (%s) but %s could not be recorded: %s", c.id, out.Reason, strings.Join(out.Pending, ", "), firstLine(out.Why)))
	return trouble
}

// UnrecordedDoneKey is the key in the tracker's key-value store under which
// mw dispatch remembers which parts of a story's unrecorded outcome it has
// written, comma between: the file itself is evidence and is never rewritten.
func UnrecordedDoneKey(id string) string { return "unrecorded." + id }

// replayUnrecorded writes down what a close-out left in the run as unrecorded,
// when the story's session is dead: the comment, the run=blocked state and the
// mail to the Mayor, each once. It returns whether the story has such a record
// for its current attempt; if so the story is a refusal like any other and is
// never refunded, whether or not every part could be written this time. A part
// that cannot be written is a note and is tried again on the next tick.
func (d Dispatch) replayUnrecorded(ctx context.Context, detail StoryDetail, report *DispatchReport) bool {
	id := detail.Story.ID
	if d.Boot.Vault == nil {
		return false
	}
	text, err := d.Boot.Vault.ReadRunFile(ctx, id, UnrecordedFileName)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: its run's record of an unrecorded close-out could not be read, so its claim was kept: %v", id, err))
			return true
		}
		return false
	}
	var out UnrecordedOutcome
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: its run's record of an unrecorded close-out could not be understood, so its claim was kept: %v", id, err))
		return true
	}
	if out.Attempt != max(1, detail.Attempts) {
		return false
	}
	if d.Memory == nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: it has an unrecorded close-out to replay, but there is nowhere to remember what was replayed, so its claim was kept", id))
		return true
	}
	done, err := d.Memory.Note(ctx, UnrecordedDoneKey(id))
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: what was replayed of its unrecorded close-out could not be read, so its claim was kept: %v", id, err))
		return true
	}
	have := map[string]bool{}
	for _, part := range strings.Split(done, ",") {
		have[part] = true
	}
	mark := func(part string) {
		have[part] = true
		var all []string
		for _, p := range []string{UnrecordedComment, UnrecordedRunState, UnrecordedMail} {
			if have[p] {
				all = append(all, p)
			}
		}
		if err := d.Memory.SetNote(ctx, UnrecordedDoneKey(id), strings.Join(all, ",")); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: that its %s was replayed could not be remembered: %v", id, part, err))
		}
	}

	replayed := fmt.Sprintf("mw dispatch on %s found %s with a dead pane and the record its close-out left of %s (%s), and wrote it down late: ", d.Host, id, out.Outcome, out.Reason)
	for _, part := range []string{UnrecordedComment, UnrecordedRunState, UnrecordedMail} {
		if !out.pending(part) || have[part] {
			continue
		}
		var err error
		switch part {
		case UnrecordedComment:
			err = d.Tracker.CommentOnStory(ctx, id, replayed+"\n\n"+out.Comment)
		case UnrecordedRunState:
			err = d.Tracker.SetStoryState(ctx, id, RunState, RunBlocked, out.StateDetail)
		case UnrecordedMail:
			if d.Mailbox == nil {
				err = errors.New("this mw dispatch has no mailbox")
				break
			}
			_, err = d.Mailbox.Send(ctx, NewMessage{From: out.MailFrom, To: out.MailTo, Subject: out.MailSubject, Body: out.MailBody})
		}
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s: the %s of its unrecorded close-out could not be replayed, tried again next tick: %v", id, part, err))
			continue
		}
		mark(part)
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the %s of its unrecorded close-out (%s) was replayed", id, part, out.Reason))
	}
	return true
}

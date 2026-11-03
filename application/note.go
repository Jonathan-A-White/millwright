package application

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// NoteStampToken is what a person types in a note where the time goes.
const NoteStampToken = "{now}"

// NoteTimeLayout is how mw note writes the time: UTC to the minute, like
// 2026-10-13 06:30Z.
const NoteTimeLayout = "2006-01-02 15:04Z"

// What a note to the Builder of a running story is called, and how its session
// is asked to answer it (mw-gq6.892).
const (
	// NoteToBuilderMark opens the comment a note to the Builder is written as:
	// the boot of every later attempt of the story reads the comments that carry
	// it (BuilderNotesIn), so a note that could not be typed still leads the next
	// attempt.
	NoteToBuilderMark = "NOTE TO THE BUILDER:"
	// NoteReplyMark opens the one comment the session is asked to answer with,
	// which is where the reply to a note is recorded.
	NoteReplyMark = "NOTE REPLY:"
)

// NoteDelivery is what came of getting a note to the Builder.
type NoteDelivery string

const (
	// NoteTyped: the line was typed into the story's live session.
	NoteTyped NoteDelivery = "typed"
	// NoteNextAttempt: no session was live, so the note leads the next attempt.
	NoteNextAttempt NoteDelivery = "next attempt"
	// NoteNotTyped: a session may be live but the line could not be typed into
	// it; the note is on the bead and leads the next attempt all the same.
	NoteNotTyped NoteDelivery = "not typed"
)

// BuilderReach types a line into a story's session on another host: the host's
// mw does it, over ssh.
type BuilderReach interface {
	Type(ctx context.Context, host, storyID, line string) error
}

// BuilderLine is what is typed into a Builder's session for a note: the note
// on one line, where it is on the bead and how to answer it.
func BuilderLine(storyID, text string) string {
	return fmt.Sprintf("NOTE FROM THE MAYOR on %s: %s -- Read this before your next step: it comes after your story and wins where they differ. "+
		"It is also the newest comment on %s. Answer it with one comment on %s that begins %s and says what you changed.\n",
		storyID, pauseOneLine(text), storyID, storyID, NoteReplyMark)
}

// BuilderNotesIn is the text of every note to the Builder among comments,
// oldest first, with the mark taken off.
func BuilderNotesIn(comments []Comment) []string {
	var notes []string
	for _, c := range comments {
		if text, ok := strings.CutPrefix(strings.TrimSpace(c.Text), NoteToBuilderMark); ok {
			notes = append(notes, strings.TrimSpace(text))
		}
	}
	return notes
}

// Note adds a comment to a bead with the text given, every {now} in it
// replaced by the clock's UTC time at the moment of writing, so that a seat
// never types a time. Text without {now} is written as it is. RunFile reads
// the text from a file first.
//
// Every refusal comes before the write: a bead the tracker does not know, a
// file that cannot be read and a note with no text change nothing.
type Note struct {
	Tracker WorkTracker

	// Now is the clock the time is read from. The zero value reads the real one.
	Now func() time.Time
	// ReadFile reads the text of RunFile. The zero value reads the disk.
	ReadFile func(path string) ([]byte, error)
	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer

	// ToBuilder writes the note as one to the Builder of the story (the comment
	// opens NoteToBuilderMark) and, when a session works the story, types it
	// into the session. Host is this host; Runner is its sessions; Reach types
	// into a story's session on another host. A nil Runner types nothing here
	// and a nil Reach nothing elsewhere.
	ToBuilder bool
	Host      string
	Runner    Runner
	Reach     BuilderReach
}

// NoteReport is what one run of mw note wrote.
type NoteReport struct {
	BeadID string
	// Text is the comment as written, {now} replaced.
	Text string
	// Time is the time every {now} was replaced with.
	Time string
	// Delivery says how a note to the Builder got there, empty for a plain
	// note; Session and Host are where it was typed and Why is what went wrong.
	Delivery NoteDelivery
	Session  string
	Host     string
	Why      string
}

// Run adds text to bead id as a comment.
func (n Note) Run(ctx context.Context, id, text string) (NoteReport, error) {
	report := NoteReport{BeadID: strings.TrimSpace(id)}
	if n.Tracker == nil {
		return report, fmt.Errorf("noting on a bead: there is no work tracker to note it in")
	}
	if report.BeadID == "" {
		return report, fmt.Errorf("noting on a bead: which bead?")
	}
	if strings.TrimSpace(text) == "" {
		return report, fmt.Errorf("noting on %s: say the text of the note", report.BeadID)
	}
	story, err := n.Tracker.ShowStory(ctx, report.BeadID)
	if err != nil {
		return report, fmt.Errorf("noting on %s: %w", report.BeadID, err)
	}

	now := time.Now
	if n.Now != nil {
		now = n.Now
	}
	report.Time = now().UTC().Format(NoteTimeLayout)
	report.Text = strings.ReplaceAll(text, NoteStampToken, report.Time)
	if n.ToBuilder {
		report.Text = NoteToBuilderMark + " " + report.Text
	}

	if err := n.Tracker.CommentOnStory(ctx, report.BeadID, report.Text); err != nil {
		return report, fmt.Errorf("noting on %s: %w", report.BeadID, err)
	}
	if n.ToBuilder {
		n.deliver(ctx, &report, story)
	}
	if n.Out != nil {
		fmt.Fprint(n.Out, report.String())
	}
	return report, nil
}

// deliver types a note to the Builder into the story's session when one is
// live, and records on the bead what came of it. A delivery that fails is
// reported, not an error: the note is on the bead and leads the next attempt.
func (n Note) deliver(ctx context.Context, report *NoteReport, story StoryDetail) {
	id := report.BeadID
	report.Session = SessionName(id)
	report.Host = ClaimHost(story.Assignee)
	if report.Host == "" {
		report.Host = story.Merged().Host
	}
	working := strings.EqualFold(strings.TrimSpace(story.Status), StatusInProgress) || LiveClaim(n.now(), story)
	line := BuilderLine(id, report.Text[len(NoteToBuilderMark)+1:])

	switch {
	case !working || report.Host == "" || report.Host == domain.HostAuto:
		report.Delivery = NoteNextAttempt
	case report.Host == n.Host:
		if n.Runner == nil {
			report.Delivery, report.Why = NoteNotTyped, "this host has no runner to type with"
			break
		}
		status, err := n.Runner.Status(ctx, report.Session)
		switch {
		case err == nil && !status.Running():
			report.Delivery = NoteNextAttempt
		default:
			// A session that could not be asked about is not known to be gone
			// (mw-gq6.886): the line is typed all the same.
			if err := n.Runner.Send(ctx, report.Session, line); err != nil {
				report.Delivery, report.Why = NoteNotTyped, err.Error()
			} else {
				report.Delivery = NoteTyped
			}
		}
	case n.Reach == nil:
		report.Delivery, report.Why = NoteNotTyped, "the story runs on "+report.Host+" and there is no way to reach it from here"
	default:
		if err := n.Reach.Type(ctx, report.Host, id, line); err != nil {
			report.Delivery, report.Why = NoteNotTyped, err.Error()
		} else {
			report.Delivery = NoteTyped
		}
	}

	var record string
	switch report.Delivery {
	case NoteTyped:
		record = fmt.Sprintf("mw note: typed into the session %s on %s at %s. The session is asked to reply with a comment that begins %s.",
			report.Session, report.Host, report.Time, NoteReplyMark)
	case NoteNotTyped:
		record = fmt.Sprintf("mw note: could not be typed into the session %s on %s (%s). It leads the next attempt.",
			report.Session, report.Host, report.Why)
	default:
		record = "mw note: no session is live on this story. It leads the next attempt."
	}
	// The note is already written; a failed record changes nothing about it.
	if err := n.Tracker.CommentOnStory(ctx, id, record); err != nil {
		report.Why = joinNote(report.Why, "the record of the delivery could not be written: "+err.Error())
	}
}

// TypeHere types line into the session of story id on this host, as the home's
// note asks it over ssh (BuilderReach). A session that is not there is an
// error, so that the home records the note as not typed.
func (n Note) TypeHere(ctx context.Context, id, line string) error {
	if n.Runner == nil {
		return fmt.Errorf("typing into the session of %s: this host has no runner", id)
	}
	name := SessionName(strings.TrimSpace(id))
	status, err := n.Runner.Status(ctx, name)
	if err == nil && !status.Running() {
		return fmt.Errorf("typing into the session of %s: there is no live session %s on this host", id, name)
	}
	if err := n.Runner.Send(ctx, name, line); err != nil {
		return fmt.Errorf("typing into the session of %s: %w", id, err)
	}
	return nil
}

func (n Note) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

// RunFile adds the text of the file at path to bead id as a comment.
func (n Note) RunFile(ctx context.Context, id, path string) (NoteReport, error) {
	read := os.ReadFile
	if n.ReadFile != nil {
		read = n.ReadFile
	}
	body, err := read(path)
	if err != nil {
		return NoteReport{BeadID: strings.TrimSpace(id)}, fmt.Errorf("noting on %s: reading the text: %w", strings.TrimSpace(id), err)
	}
	return n.Run(ctx, id, strings.TrimRight(string(body), "\r\n"))
}

// String is the report as a person reads it: the time used.
func (r NoteReport) String() string {
	line := fmt.Sprintf("noted on %s at %s\n", r.BeadID, r.Time)
	switch r.Delivery {
	case NoteTyped:
		line += fmt.Sprintf("typed into the session %s on %s; its reply is the comment that begins %s\n", r.Session, r.Host, NoteReplyMark)
	case NoteNotTyped:
		line += fmt.Sprintf("could not be typed into the session %s on %s: %s; it leads the next attempt\n", r.Session, r.Host, r.Why)
	case NoteNextAttempt:
		line += "no session is live: it leads the next attempt\n"
	}
	return line
}

package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// The bounds of mw tidy: everything it will ever do is one of the three rows
// of docs/tidy.md, at these ages. Widening one is a change to this file and to
// that table together.
const (
	// TidyAnswerAge is how old an "Answer: ..." mail bead must be before mw
	// tidy closes it: the answer was also commented on its bead, so the mail
	// is only a nudge that has been seen or never will be.
	TidyAnswerAge = 24 * time.Hour

	// TidyReadAge is how old a mail bead the mailbox holds read must be before
	// mw tidy closes it.
	TidyReadAge = 7 * 24 * time.Hour

	// TidyAnswerSubject is what the subject of an Answer mail starts with: the
	// mail Postern's inbox sends the Mayor when the Governor answers a
	// question.
	TidyAnswerSubject = "Answer: "

	// TidyWhy starts the one line every act writes on what it touched.
	TidyWhy = "Tidied by mw tidy: "

	// TidyLogPrefix starts the words of a tick log line mw tidy writes, one
	// per act. It is not a run of the tick: MillhandTickOutcome and the resume
	// rules read past it.
	TidyLogPrefix = "tidy: "
)

// TidyMail is one open mail bead as mw tidy looks at it.
type TidyMail struct {
	ID      string
	Subject string
	// Sent is when the mail was filed; zero when the mailbox did not say, and
	// mail of no known age is never old enough.
	Sent time.Time
	// Read is whether the mailbox holds the mail read.
	Read bool
}

// TidyMailbox is the part of the mailbox mw tidy works through: it is Mailbox's
// own beads, narrowed to the two things tidy may do to mail.
type TidyMailbox interface {
	// OpenMail lists every mail bead that is still open, whoever it is for. It
	// lists mail and nothing else, and reads and writes nothing.
	OpenMail(ctx context.Context) ([]TidyMail, error)

	// CloseMail closes one mail bead with a reason. An id that names a bead
	// that is not mail — a story, an epic, a map, a hitl bead — is an error
	// and nothing is changed.
	CloseMail(ctx context.Context, id, reason string) error
}

// TidyNotes is the part of the tracker's key-value store mw tidy clears a
// question note from: TrackerSync's own notes, narrowed.
type TidyNotes interface {
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
	ClearNote(ctx context.Context, key string) error
}

// TidyBeads is the part of the work tracker mw tidy reads a question's bead
// through, and says what it did on it. It never closes one.
type TidyBeads interface {
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
	CommentOnStory(ctx context.Context, id, text string) error
}

// The kinds of act mw tidy makes: the three rows of docs/tidy.md.
const (
	TidyAnswerMail   = "answer mail"
	TidyReadMail     = "read mail"
	TidyQuestionNote = "question note"
)

// Tidy closes what is plainly finished, within fixed bounds, and nothing else:
// an Answer mail bead over TidyAnswerAge old, a mail bead read and over
// TidyReadAge old, and a postern question note whose bead is closed. It never
// closes a story, an epic, a map or a hitl bead, and never deletes anything;
// the mail it closes stays in the tracker, closed. Each act writes one line on
// what it touched and one to the tick log. A dry run says what it would do
// and changes nothing.
type Tidy struct {
	Mail  TidyMailbox
	Notes TidyNotes
	Beads TidyBeads

	// Log is the tick log each act adds a line to; nil writes none.
	Log TickLog

	DryRun bool

	// Now is the clock ages are measured against; nil is time.Now.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// TidyAct is one thing mw tidy did, or in a dry run would do.
type TidyAct struct {
	Kind string
	// ID is the mail bead closed, or the bead whose question note was cleared.
	ID  string
	Why string
}

// String is the act as a line of the log and the report: what it touched and
// the line written on it.
func (a TidyAct) String() string { return a.Kind + " " + a.ID + ": " + a.Why }

// TidyReport is what one tidy pass did.
type TidyReport struct {
	DryRun bool
	Acts   []TidyAct
	// Notes are the things that went sideways without stopping the pass.
	Notes []string
}

// Run makes one pass. A look that fails, or an act that does, is a note and
// the pass goes on to the rest; Run returns an error when any did, after
// doing what it could.
func (t Tidy) Run(ctx context.Context) (TidyReport, error) {
	report := TidyReport{DryRun: t.DryRun}
	if t.Mail == nil || t.Notes == nil || t.Beads == nil {
		return report, fmt.Errorf("tidying: there is no mailbox, note store or tracker to tidy through")
	}

	var faults []error
	fault := func(err error) {
		faults = append(faults, err)
		report.Notes = append(report.Notes, oneLine(err.Error()))
	}
	if err := t.mail(ctx, &report); err != nil {
		fault(err)
	}
	if err := t.questions(ctx, &report); err != nil {
		fault(err)
	}

	t.print(report.String())
	return report, errors.Join(faults...)
}

// mail closes the mail the first two rows of docs/tidy.md name.
func (t Tidy) mail(ctx context.Context, report *TidyReport) error {
	open, err := t.Mail.OpenMail(ctx)
	if err != nil {
		return fmt.Errorf("reading the open mail: %w", err)
	}
	var faults []error
	for _, mail := range open {
		kind, why := t.mailRow(mail)
		if kind == "" {
			continue
		}
		act := TidyAct{Kind: kind, ID: mail.ID, Why: TidyWhy + why}
		if !t.DryRun {
			if err := t.Mail.CloseMail(ctx, mail.ID, act.Why); err != nil {
				faults = append(faults, fmt.Errorf("closing the mail %s: %w", mail.ID, err))
				continue
			}
			t.log(ctx, act, report)
		}
		report.Acts = append(report.Acts, act)
	}
	return errors.Join(faults...)
}

// mailRow says which row of docs/tidy.md, if any, lets one mail be closed, and
// why. An Answer mail is closed by the first row whether it is read or not.
func (t Tidy) mailRow(mail TidyMail) (kind, why string) {
	if mail.Sent.IsZero() {
		return "", ""
	}
	age := t.now().Sub(mail.Sent)
	switch {
	case strings.HasPrefix(mail.Subject, TidyAnswerSubject) && age > TidyAnswerAge:
		return TidyAnswerMail, "an Answer mail over 1 day old"
	case mail.Read && age > TidyReadAge:
		return TidyReadMail, "a mail read and over 7 days old"
	}
	return "", ""
}

// questions clears the question notes the third row of docs/tidy.md names. The
// note is the only thing it clears; the bead it belongs to is commented on and
// never changed.
func (t Tidy) questions(ctx context.Context, report *TidyReport) error {
	prefix := PosternQuestionKey("")
	notes, err := t.Notes.NotesWithPrefix(ctx, prefix)
	if err != nil {
		return fmt.Errorf("reading the question notes: %w", err)
	}
	var ids []string
	for key := range notes {
		if id := strings.TrimPrefix(key, prefix); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	beads, err := t.Beads.ShowBeads(ctx, ids)
	if err != nil {
		return fmt.Errorf("reading the beads the questions were asked about: %w", err)
	}
	var faults []error
	for _, bead := range beads {
		// A bead the tracker did not return is left out, and its note with it.
		if !bead.Closed() {
			continue
		}
		id := bead.Story.ID
		act := TidyAct{Kind: TidyQuestionNote, ID: id, Why: TidyWhy + "its question note outlived the closed bead"}
		if !t.DryRun {
			if err := t.Beads.CommentOnStory(ctx, id, act.Why); err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("%s could not be commented on: %s", id, oneLine(err.Error())))
			}
			if err := t.Notes.ClearNote(ctx, PosternQuestionKey(id)); err != nil {
				faults = append(faults, fmt.Errorf("clearing the question note of %s: %w", id, err))
				continue
			}
			t.log(ctx, act, report)
		}
		report.Acts = append(report.Acts, act)
	}
	return errors.Join(faults...)
}

// log adds the act's line to the tick log, dated as the tick's own are. A log
// that cannot be written is a note: the act is done.
func (t Tidy) log(ctx context.Context, act TidyAct, report *TidyReport) {
	if t.Log == nil {
		return
	}
	line := t.now().UTC().Format(time.RFC3339) + " " + TidyLogPrefix + act.String()
	if err := t.Log.Append(ctx, line); err != nil {
		report.Notes = append(report.Notes, "the tick log could not be written: "+oneLine(err.Error()))
	}
}

func (t Tidy) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

func (t Tidy) print(text string) {
	if t.Out != nil {
		fmt.Fprint(t.Out, text)
	}
}

// Summary is the report in a phrase, for the tick's line: what was done, or
// in a dry run what would be.
func (r TidyReport) Summary() string {
	verb := "tidied "
	if r.DryRun {
		verb = "would tidy "
	}
	return verb + counted(len(r.Acts), "thing", "things")
}

// String is the report as a person reads it: no line wider than Width.
func (r TidyReport) String() string {
	var b strings.Builder
	title := "mw tidy"
	if r.DryRun {
		title += " · dry run"
	}
	clip(&b, title)
	if len(r.Acts) == 0 {
		clip(&b, "  nothing plainly finished")
	}
	verb := "closed "
	if r.DryRun {
		verb = "would close "
	}
	for _, act := range r.Acts {
		what := verb
		if act.Kind == TidyQuestionNote {
			what = strings.Replace(what, "close", "clear", 1)
		}
		clip(&b, "  "+what+act.String())
	}
	for _, note := range r.Notes {
		clip(&b, "  ! "+note)
	}
	return b.String()
}

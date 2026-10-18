package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// The bounds of mw tidy: everything it will ever do is one of the four rows
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

	// TidyUnansweredMarker starts the comment mw tidy leaves on a closed bead
	// when it has mailed the Mayor about a decision card nobody answered; the
	// card's txid follows. Its presence is how a second pass knows not to mail
	// again.
	TidyUnansweredMarker = TidyWhy + "its decision card has no answer, so it is left in place and the Mayor is mailed"

	// TidyUnansweredSubject starts the subject of that mail.
	TidyUnansweredSubject = "Unanswered card on closed "
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

	// Send files one message, as Mailbox does: mw tidy mails the Mayor about a
	// decision card nobody answered.
	Send(ctx context.Context, message NewMessage) (string, error)
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
	StoryComments(ctx context.Context, id string) ([]Comment, error)
	CommentOnStory(ctx context.Context, id, text string) error
}

// PouredFormula is one formula poured for a story that still has a bead open,
// as mw tidy reads it.
type PouredFormula struct {
	// Root is the bead the steps hang from.
	Root string
	// RootOpen says the root itself is still open.
	RootOpen bool
	// Steps are its steps still open, in the order they are closed.
	Steps []string
	// Story is the story it was poured for, as the title of its first step
	// names it (FormulaStoryOf); empty when no step names one.
	Story string
	// LiveStory is a story not closed whose metadata records Root as its
	// molecule; empty when none does.
	LiveStory string
}

// TidyFormulas is the part of the work tracker mw tidy closes what a poured
// formula left open through. It closes the beads of formulas and nothing else.
type TidyFormulas interface {
	// OpenFormulas lists every poured formula with a bead still open. It reads
	// and writes nothing else.
	OpenFormulas(ctx context.Context) ([]PouredFormula, error)

	// CloseFormulaBead closes one bead of a poured formula, a step or a root,
	// with a reason. An id that names a bead that is not a formula's — a
	// story, an epic, a mail — is an error and nothing is changed.
	CloseFormulaBead(ctx context.Context, id, reason string) error
}

// The kinds of act mw tidy makes: the four rows of docs/tidy.md, the fourth
// closing a formula's steps and then its root.
const (
	TidyAnswerMail   = "answer mail"
	TidyReadMail     = "read mail"
	TidyQuestionNote = "question note"
	TidyFormulaStep  = "formula step"
	TidyFormulaRoot  = "formula"

	// TidyUnansweredCard is the act on a decision card nobody answered whose
	// bead is closed: the Mayor is mailed and the card stays.
	TidyUnansweredCard = "unanswered card"
)

// Tidy closes what is plainly finished, within fixed bounds, and nothing else:
// an Answer mail bead over TidyAnswerAge old, a mail bead read and over
// TidyReadAge old, a postern question note whose bead is closed, and the beads
// left open of a formula whose story is closed. A question note that is a
// decision card nobody answered is not cleared: the Mayor is mailed once and the
// card stays (mw-gq6.618). It never closes a story, an
// epic, a map or a hitl bead, and never deletes anything;
// the mail it closes stays in the tracker, closed. Each act writes one line on
// what it touched and one to the tick log. A dry run says what it would do
// and changes nothing.
type Tidy struct {
	Mail  TidyMailbox
	Notes TidyNotes
	Beads TidyBeads
	// Formulas is what the formula beads of closed stories are closed
	// through; nil leaves formulas alone.
	Formulas TidyFormulas

	// Host is the host mw tidy runs on, the mail to the Mayor is from.
	Host string

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
	if err := t.formulas(ctx, &report); err != nil {
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
		if note, isCard := decisionCardOf(notes[PosternQuestionKey(id)]); isCard {
			held, err := t.unansweredCard(ctx, bead, note, report)
			if err != nil {
				faults = append(faults, err)
				continue
			}
			if held {
				continue
			}
		}
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

// decisionCardOf reads a question note as a decision card: one that recorded the
// txid it was asked under or the question. A note that is neither, "{}" or
// anything undecodable, is a stale mark and not a card.
func decisionCardOf(value string) (posternQuestionNote, bool) {
	var note posternQuestionNote
	if err := json.Unmarshal([]byte(value), &note); err != nil {
		return posternQuestionNote{}, false
	}
	return note, strings.TrimSpace(note.Txid) != "" || strings.TrimSpace(note.Q) != ""
}

// unansweredCard says whether the decision card of a closed bead is held, that
// is left in place for the Mayor. It is held when no ANSWER comment follows the
// bead's newest QUESTION comment. The first time, the Mayor is mailed and the
// bead is commented with TidyUnansweredMarker and the card's txid; a pass that
// finds that comment mails no one again. An error means the comments could not
// be read or the Mayor could not be mailed: the card is left, unmailed, for the
// next pass.
func (t Tidy) unansweredCard(ctx context.Context, bead StoryDetail, note posternQuestionNote, report *TidyReport) (held bool, err error) {
	id := bead.Story.ID
	comments, err := t.Beads.StoryComments(ctx, id)
	if err != nil {
		return false, fmt.Errorf("reading the comments of %s to see whether its card was answered: %w", id, err)
	}
	if cardAnswered(comments) {
		return false, nil
	}
	marker := TidyUnansweredMarker + " (txid " + strings.TrimSpace(note.Txid) + ")"
	for _, c := range comments {
		if c.Text == marker {
			return true, nil
		}
	}
	act := TidyAct{Kind: TidyUnansweredCard, ID: id, Why: TidyWhy + "its decision card has no answer and was left in place"}
	if !t.DryRun {
		question := note.Q
		if facts, ok := questionFromComments(comments); question == "" && ok {
			question = facts.Text
		}
		first, _, _ := strings.Cut(strings.TrimSpace(question), "\n")
		if first == "" {
			first = "(the question text was not recorded)"
		}
		body := fmt.Sprintf("%s (%s) was closed with a decision card nobody answered, txid %s.\n\nQuestion: %s\nOptions: %s\n\n"+
			"mw tidy left the card in place. Re-ask it on an open bead, or clear its note by hand with a reason.",
			id, bead.Story.Title, strings.TrimSpace(note.Txid), strings.TrimSpace(question), strings.Join(note.Options, ", "))
		if _, err := t.Mail.Send(ctx, NewMessage{
			From:    SeatIdentity(MwSeat, t.Host),
			To:      MayorMailbox,
			Subject: TidyUnansweredSubject + id + ": " + clippedTo(strings.Join(strings.Fields(first), " "), PosternAnswerSubjectLimit),
			Body:    body,
		}); err != nil {
			return false, fmt.Errorf("mailing the Mayor about the unanswered card on %s: %w", id, err)
		}
		if err := t.Beads.CommentOnStory(ctx, id, marker); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("%s could not be commented on, so the Mayor may be mailed again: %s", id, oneLine(err.Error())))
		}
		t.log(ctx, act, report)
	}
	report.Acts = append(report.Acts, act)
	return true, nil
}

// cardAnswered reports whether an ANSWER comment follows the newest QUESTION
// comment of comments (oldest first), or any does when no QUESTION is there.
func cardAnswered(comments []Comment) bool {
	asked := -1
	for i, c := range comments {
		if strings.HasPrefix(c.Text, "QUESTION ") {
			asked = i
		}
	}
	for _, c := range comments[asked+1:] {
		if strings.HasPrefix(c.Text, "ANSWER ") {
			return true
		}
	}
	return false
}

// formulas closes what the fourth row of docs/tidy.md names: the steps left
// open, and then the root, of a formula poured for a story that is closed. A
// formula whose story no step names, or that a story not closed records as its
// own, or whose story the tracker does not return closed, is left as it is: a
// step of an open or running story is never closed.
func (t Tidy) formulas(ctx context.Context, report *TidyReport) error {
	if t.Formulas == nil {
		return nil
	}
	open, err := t.Formulas.OpenFormulas(ctx)
	if err != nil {
		return fmt.Errorf("reading the open formulas: %w", err)
	}
	var candidates []PouredFormula
	seen := map[string]bool{}
	var stories []string
	for _, formula := range open {
		if formula.Story == "" || formula.LiveStory != "" {
			continue
		}
		candidates = append(candidates, formula)
		if !seen[formula.Story] {
			seen[formula.Story] = true
			stories = append(stories, formula.Story)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Strings(stories)
	beads, err := t.Beads.ShowBeads(ctx, stories)
	if err != nil {
		return fmt.Errorf("reading the stories formulas were poured for: %w", err)
	}
	closed := map[string]bool{}
	for _, bead := range beads {
		closed[bead.Story.ID] = bead.Closed()
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Root < candidates[j].Root })

	var faults []error
	for _, formula := range candidates {
		if !closed[formula.Story] {
			continue
		}
		why := TidyWhy + "its story " + formula.Story + " is closed"
		acts := make([]TidyAct, 0, len(formula.Steps)+1)
		for _, step := range formula.Steps {
			acts = append(acts, TidyAct{Kind: TidyFormulaStep, ID: step, Why: why})
		}
		if formula.RootOpen {
			acts = append(acts, TidyAct{Kind: TidyFormulaRoot, ID: formula.Root, Why: why})
		}
		for _, act := range acts {
			if !t.DryRun {
				// A root is never closed over a step that would not close.
				if err := t.Formulas.CloseFormulaBead(ctx, act.ID, act.Why); err != nil {
					faults = append(faults, fmt.Errorf("closing the %s %s: %w", act.Kind, act.ID, err))
					break
				}
				t.log(ctx, act, report)
			}
			report.Acts = append(report.Acts, act)
		}
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
		switch act.Kind {
		case TidyQuestionNote:
			what = strings.Replace(what, "close", "clear", 1)
		case TidyUnansweredCard:
			what = "mailed "
			if r.DryRun {
				what = "would mail "
			}
		}
		clip(&b, "  "+what+act.String())
	}
	for _, note := range r.Notes {
		clip(&b, "  ! "+note)
	}
	return b.String()
}

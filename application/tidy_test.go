package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A mailbox that cannot be read does not stop the question notes being tidied,
// and the pass says it failed.
func TestTidyGoesOnToTheQuestionNotesWhenTheMailCannotBeRead(t *testing.T) {
	ctx := context.Background()
	mail := apptest.NewFakeMailbox()
	mail.Err = errors.New("the mail store is not answering")
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("t", domain.Path{})
	tracker.AddStory("t", domain.Story{ID: "t.1", Title: "t.1"})
	if err := tracker.SetStatus("t.1", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(ctx, application.PosternQuestionKey("t.1"), "{}"); err != nil {
		t.Fatal(err)
	}

	report, err := application.Tidy{Mail: mail, Notes: tracker, Beads: tracker}.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "the mail store is not answering") {
		t.Fatalf("expected the mail fault to be returned, got %v", err)
	}
	if len(report.Acts) != 1 || report.Acts[0].Kind != application.TidyQuestionNote {
		t.Errorf("expected the question note to be tidied all the same, got %+v", report.Acts)
	}
	if len(report.Notes) != 1 {
		t.Errorf("expected one note on the fault, got %q", report.Notes)
	}
}

// Mail of no known age is never old enough, and an Answer mail that is exactly
// at its bound is not yet past it.
func TestTidyLeavesMailOfUnknownAgeAndMailAtItsBound(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	mail := apptest.NewFakeMailbox()
	tracker := apptest.NewFakeTracker()
	unknown, _ := mail.Send(ctx, application.NewMessage{From: "a", To: "mayor", Subject: "Answer: x: y", Body: "b"})
	if err := mail.SetSent(unknown, time.Time{}); err != nil {
		t.Fatal(err)
	}
	atBound, _ := mail.Send(ctx, application.NewMessage{From: "a", To: "mayor", Subject: "Answer: x: z", Body: "b"})
	if err := mail.SetSent(atBound, now.Add(-application.TidyAnswerAge)); err != nil {
		t.Fatal(err)
	}

	report, err := application.Tidy{Mail: mail, Notes: tracker, Beads: tracker, Now: func() time.Time { return now }}.Run(ctx)
	if err != nil || len(report.Acts) != 0 {
		t.Errorf("expected nothing tidied and no fault, got %+v, %v", report.Acts, err)
	}
}

// stubFormulas is a tracker's formulas as mw tidy reads them, set by hand, and
// what it was asked to close.
type stubFormulas struct {
	open   []application.PouredFormula
	closed []string
}

func (s *stubFormulas) OpenFormulas(context.Context) ([]application.PouredFormula, error) {
	return s.open, nil
}

func (s *stubFormulas) CloseFormulaBead(_ context.Context, id, _ string) error {
	s.closed = append(s.closed, id)
	return nil
}

// A formula is closed only when the story its first step names is closed and
// no story not closed records it as its own: a formula that names no story, one
// a live story records whatever story its steps name, and one whose story the
// tracker does not return are all left as they are (mw-gq6.438).
func TestTidyClosesAFormulaOnlyOfAStoryItCanReadClosed(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("t", domain.Path{})
	tracker.AddStory("t", domain.Story{ID: "t.1", Title: "t.1"})
	if err := tracker.SetStatus("t.1", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	formulas := &stubFormulas{open: []application.PouredFormula{
		{Root: "t-mol-named", RootOpen: true, Steps: []string{"t-mol-n1"}, Story: "t.1"},
		{Root: "t-mol-nameless", RootOpen: true, Steps: []string{"t-mol-x1"}},
		{Root: "t-mol-live", RootOpen: true, Steps: []string{"t-mol-l1"}, Story: "t.1", LiveStory: "t.2"},
		{Root: "t-mol-unknown", RootOpen: true, Steps: []string{"t-mol-u1"}, Story: "t.9"},
	}}

	report, err := application.Tidy{Mail: apptest.NewFakeMailbox(), Notes: tracker, Beads: tracker, Formulas: formulas}.Run(ctx)
	if err != nil {
		t.Fatalf("tidying: %v", err)
	}
	if want := []string{"t-mol-n1", "t-mol-named"}; strings.Join(formulas.closed, " ") != strings.Join(want, " ") {
		t.Errorf("closed %q, want only the closed story's step and then its root, %q", formulas.closed, want)
	}
	if len(report.Acts) != 2 || report.Acts[0].Kind != application.TidyFormulaStep || report.Acts[1].Kind != application.TidyFormulaRoot {
		t.Errorf("expected a step act and then a root act, got %+v", report.Acts)
	}
}

func TestFormulaStoryOfReadsTheStoryTheFirstStepNames(t *testing.T) {
	for title, want := range map[string]string{
		"Understand mw-gq6.438: [friction] mw closes a story's formula steps": "mw-gq6.438",
		"Do mw-tbx1n: a chore":  "mw-tbx1n",
		"Implement until green": "",
		"Commit":                "",
		"Write the closing comment: and memory notes": "",
		"Understand mw-mol-abc: not a story":          "",
	} {
		if got := application.FormulaStoryOf(title); got != want {
			t.Errorf("FormulaStoryOf(%q) = %q, want %q", title, got, want)
		}
	}
}

// A decision card nobody answered on a closed bead is kept, and a Mayor who
// cannot be mailed leaves it unmarked so the next pass tries again
// (mw-gq6.618).
func TestTidyKeepsAnUnansweredCardWhenTheMayorCannotBeMailed(t *testing.T) {
	ctx := context.Background()
	mail := apptest.NewFakeMailbox()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("t", domain.Path{})
	tracker.AddStory("t", domain.Story{ID: "t.1", Title: "t.1"})
	if err := tracker.CommentOnStory(ctx, "t.1", "QUESTION 2026-10-16T11:00:38Z asked by postern, txid abc: Squash? (recommended A; options A, B)"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStatus("t.1", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(ctx, application.PosternQuestionKey("t.1"), `{"txid":"abc","q":"Squash?"}`); err != nil {
		t.Fatal(err)
	}
	mail.Err = errors.New("the mail store is not answering")

	_, err := application.Tidy{Mail: mail, Notes: tracker, Beads: tracker}.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "mailing the Mayor") {
		t.Fatalf("expected the mail fault, got %v", err)
	}
	notes, _ := tracker.NotesWithPrefix(ctx, application.PosternQuestionKey(""))
	if _, there := notes[application.PosternQuestionKey("t.1")]; !there {
		t.Error("the card was cleared although the Mayor was not told")
	}
	if said := tracker.Comments("t.1"); len(said) != 1 {
		t.Errorf("expected only the QUESTION comment, got %q", said)
	}
}

// A QUESTION for Monday written after the Governor answered a card is not a
// card nobody answered: mw tidy does not take it for the newest QUESTION.
func TestTidyDoesNotTakeAQuestionForMondayForAnUnansweredCard(t *testing.T) {
	ctx := context.Background()
	mail := apptest.NewFakeMailbox()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("t", domain.Path{})
	tracker.AddStory("t", domain.Story{ID: "t.1", Title: "t.1"})
	for _, c := range []string{
		"QUESTION 2026-10-16T11:00:38Z asked by postern, txid abc: Squash? (recommended A; options A, B)",
		"ANSWER 2026-10-16T12:00:00Z from 02ab, txid def: A",
		application.QuestionForMondayPrefix + " Another thing?\n\nOptions:\n- A\nRecommended: A",
	} {
		if err := tracker.CommentOnStory(ctx, "t.1", c); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracker.SetStatus("t.1", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(ctx, application.PosternQuestionKey("t.1"), `{"txid":"abc","q":"Squash?"}`); err != nil {
		t.Fatal(err)
	}
	report, err := application.Tidy{Mail: mail, Notes: tracker, Beads: tracker}.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, act := range report.Acts {
		if act.Kind == application.TidyUnansweredCard {
			t.Fatalf("tidy took the held question for an unanswered card: %+v", act)
		}
	}
}

// mw-gq6.1132: mw tidy closed each bead of a closed story's formula with one
// call and no read, so a close that met a passing fault left the formula open
// for the next tidy, and one bd called indeterminate that had landed was
// reported as a fault. These tests close a formula through a tracker whose
// closes fail the way bd's do.

// tidyFormulaTracker is a FakeTracker that closes the beads of a formula the
// way a real tracker does, failing the first fails closes of each bead named in
// failing with err; with landed, a close that failed was made all the same.
type tidyFormulaTracker struct {
	*apptest.FakeTracker
	failing map[string]int
	err     error
	landed  bool
	asked   []string
}

func (f *tidyFormulaTracker) CloseFormulaBead(ctx context.Context, id, reason string) error {
	f.asked = append(f.asked, id)
	if f.failing[id] > 0 {
		f.failing[id]--
		if f.landed {
			if err := f.FakeTracker.CloseFormulaBead(ctx, id, reason); err != nil {
				return err
			}
		}
		return f.err
	}
	return f.FakeTracker.CloseFormulaBead(ctx, id, reason)
}

type tidiedFormula struct {
	tracker *tidyFormulaTracker
	mail    *apptest.FakeMailbox
	report  application.TidyReport
	err     error
	waits   []time.Duration
	root    string
	step    string
}

// aClosedStoryWhoseFormulaCloseFaults tidies the formula of a closed story, one
// step and a root, whose close of the step fails fails times with err.
func aClosedStoryWhoseFormulaCloseFaults(t *testing.T, fails int, err error, landed bool) *tidiedFormula {
	t.Helper()
	ctx := context.Background()
	fake := apptest.NewFakeTracker()
	fake.AddFormula("tdd-feature", application.FormulaStep{Title: "Understand {{story}}: {{title}}"})
	fake.AddEpic("mw-x", domain.Path{})
	fake.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A closed story"})
	poured, perr := fake.PourFormula(ctx, "tdd-feature", "mw-x.1", "A closed story", false)
	if perr != nil {
		t.Fatal(perr)
	}
	if err := fake.SetStatus("mw-x.1", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	run := &tidiedFormula{mail: apptest.NewFakeMailbox(), root: poured.RootID, step: poured.Steps[0].ID}
	run.tracker = &tidyFormulaTracker{FakeTracker: fake, failing: map[string]int{poured.Steps[0].ID: fails}, err: err, landed: landed}
	run.report, run.err = application.Tidy{
		Mail: run.mail, Notes: fake, Beads: fake, Formulas: run.tracker, Molecules: fake,
		Host: "laptop", PushWait: 20 * time.Second,
		RetryWait: func(_ context.Context, d time.Duration) error {
			run.waits = append(run.waits, d)
			return nil
		},
	}.Run(ctx)
	return run
}

func (r *tidiedFormula) leftOpen(t *testing.T) []string {
	t.Helper()
	open, err := r.tracker.OpenFormulas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, formula := range open {
		left = append(left, formula.Root)
	}
	return left
}

func (r *tidiedFormula) lostWriteMails(t *testing.T) []application.Message {
	t.Helper()
	inbox, _ := r.mail.Inbox(context.Background(), application.MayorMailbox)
	var lost []application.Message
	for _, m := range inbox {
		if strings.HasPrefix(m.Subject, "lost write") {
			lost = append(lost, m)
		}
	}
	return lost
}

// A close that met a passing fault is tried again, and the formula ends closed
// with nothing left for the next tidy.
func TestTidyRetriesAFormulaCloseThatMetAPassingFault(t *testing.T) {
	run := aClosedStoryWhoseFormulaCloseFaults(t, 2, formulaTimeout(), false)
	if run.err != nil {
		t.Fatalf("tidying: %v", run.err)
	}
	if left := run.leftOpen(t); len(left) != 0 {
		t.Errorf("expected the formula closed, %v is left open", left)
	}
	if len(run.report.Acts) != 2 {
		t.Errorf("expected a step act and a root act, got %+v", run.report.Acts)
	}
	if len(run.waits) != 2 {
		t.Errorf("expected two waits between three tries, got %v", run.waits)
	}
	if lost := run.lostWriteMails(t); len(lost) != 0 {
		t.Errorf("expected no lost-write mail, got %d", len(lost))
	}
}

// A close bd called indeterminate is read first: one that landed is done and
// not tried again.
func TestTidyReadsFirstAnIndeterminateFormulaCloseThatLanded(t *testing.T) {
	run := aClosedStoryWhoseFormulaCloseFaults(t, 1, errors.New(formulaUnsure), true)
	if run.err != nil {
		t.Fatalf("tidying: %v", run.err)
	}
	if left := run.leftOpen(t); len(left) != 0 {
		t.Errorf("expected the formula closed, %v is left open", left)
	}
	if n := timesAsked(run.tracker.asked, run.step); n != 1 {
		t.Errorf("expected the step closed once, it was asked %d times", n)
	}
	if len(run.waits) != 0 {
		t.Errorf("expected no wait for a close that had landed, got %v", run.waits)
	}
}

// A close bd called indeterminate that did not land is read open and tried
// again.
func TestTidyTriesAgainAnIndeterminateFormulaCloseThatDidNotLand(t *testing.T) {
	run := aClosedStoryWhoseFormulaCloseFaults(t, 1, errors.New(formulaUnsure), false)
	if run.err != nil {
		t.Fatalf("tidying: %v", run.err)
	}
	if left := run.leftOpen(t); len(left) != 0 {
		t.Errorf("expected the formula closed, %v is left open", left)
	}
	if n := timesAsked(run.tracker.asked, run.step); n != 2 {
		t.Errorf("expected the step closed in two tries, it was asked %d times", n)
	}
}

// A bead that fails every try is a fault of the pass and is mailed to the Mayor
// once, with the count of the tries; the root is not closed over it.
func TestTidyMailsAFormulaCloseLostEveryTryOnceWithTheCount(t *testing.T) {
	run := aClosedStoryWhoseFormulaCloseFaults(t, 100, formulaTimeout(), false)
	if run.err == nil {
		t.Fatal("expected the lost close to be a fault of the pass")
	}
	if n := timesAsked(run.tracker.asked, run.step); n != application.ReadRetries+1 {
		t.Errorf("expected %d tries, got %d", application.ReadRetries+1, n)
	}
	if n := timesAsked(run.tracker.asked, run.root); n != 0 {
		t.Errorf("expected the root not closed over a step that would not close, it was asked %d times", n)
	}
	lost := run.lostWriteMails(t)
	if len(lost) != 1 {
		t.Fatalf("expected one lost-write mail, got %d", len(lost))
	}
	if want := "tried 5 times"; !strings.Contains(lost[0].Body, want) {
		t.Errorf("expected the mail to say %q, got %q", want, lost[0].Body)
	}
}

func timesAsked(ids []string, id string) int {
	n := 0
	for _, got := range ids {
		if got == id {
			n++
		}
	}
	return n
}

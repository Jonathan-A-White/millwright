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

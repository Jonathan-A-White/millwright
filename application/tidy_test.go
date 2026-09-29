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

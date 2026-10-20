package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// ideasTodoFixture is the apply fixture with Ideas only on until an hour from
// the clock it is read at.
func ideasTodoFixture(t *testing.T) (*applyFixture, application.PosternInbox, *memoryIdeasFiles) {
	t.Helper()
	f := newApplyFixture(t)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	f.backend.IdeasOnlyNow = application.IdeasOnlyNow{On: true, Until: now.Add(time.Hour).Unix(), By: application.IdeasOnlyByUntil}
	files := &memoryIdeasFiles{}
	inbox := f.inbox()
	inbox.Ideas = &application.IdeasOnlyMode{Reader: f.backend, Files: files, GovernorKey: releaseTapGovernorKey, Now: func() time.Time { return now }}
	return f, inbox, files
}

// During Ideas only, a close of one of his own to-dos is applied at once and
// nothing is held.
func TestIdeasOnlyAppliesACloseOfAToDo(t *testing.T) {
	f, inbox, files := ideasTodoFixture(t)
	mustDo(t, f.tracker.SetLabels("mw-e.3", application.LabelHitl, application.LabelHitlTodo))
	f.action(t, "tx-done", map[string]any{"action": "close", "bead": "mw-e.3"})

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if got := f.status(t, "mw-e.3"); got != apptest.StatusClosed {
		t.Fatalf("expected the to-do mw-e.3 closed during Ideas only, got %s", got)
	}
	if len(files.held) != 0 {
		t.Fatalf("expected nothing held, got %+v", files.held)
	}
}

// A close of any other bead is still an idea, held until the mode ends.
func TestIdeasOnlyStillHoldsACloseOfAnotherBead(t *testing.T) {
	f, inbox, files := ideasTodoFixture(t)
	f.action(t, "tx-close", map[string]any{"action": "close", "bead": "mw-e.3"})

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if got := f.status(t, "mw-e.3"); got == apptest.StatusClosed {
		t.Fatalf("expected mw-e.3, not a to-do, left open during Ideas only")
	}
	if len(files.held) != 1 || files.held[0].Txid != "tx-close" {
		t.Fatalf("expected the close held, got %+v", files.held)
	}
}

// A reply on a to-do's thread is applied at once too.
func TestIdeasOnlyAppliesAReplyOnAToDo(t *testing.T) {
	f, inbox, files := ideasTodoFixture(t)
	mustDo(t, f.tracker.SetLabels("mw-e.3", application.LabelHitl, application.LabelHitlTodo))
	f.message(t, releaseTapGovernorKey, "tx-reply", `{"bead":"mw-e.3","answer":"done, thanks"}`)

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if len(files.held) != 0 {
		t.Fatalf("expected the reply on a to-do not held, got %+v", files.held)
	}
	if note, _ := f.tracker.Note(context.Background(), application.PosternAppliedKey("tx-reply")); !strings.Contains(note, "mw-e.3") {
		t.Fatalf("expected the reply marked applied, got %q", note)
	}
}

// A to-do the tracker cannot find is not known to be one, so it is held.
func TestIdeasOnlyHoldsAnActionOnABeadItCannotFind(t *testing.T) {
	f, inbox, files := ideasTodoFixture(t)
	f.action(t, "tx-gone", map[string]any{"action": "close", "bead": "mw-nope.1"})

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if len(files.held) != 1 {
		t.Fatalf("expected the action held, got %+v", files.held)
	}
}

// The banner says to-dos still work.
func TestIdeasOnlyBannerSaysToDosStillWork(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	state := application.IdeasOnlyState{On: true, Until: now.Add(time.Hour).Unix()}
	want := "Ideas only until 13:00 UTC (2 ideas held); to-dos still work"
	if got := application.IdeasOnlyBanner(state, 2, now); got != want {
		t.Fatalf("banner = %q, want %q", got, want)
	}
}

// A message about a to-do reaches the Mayor during Ideas only; any other is
// still held (mw-8ifimx.13).
func TestIdeasOnlyPassesAMessageAboutAToDoAndHoldsAnIdea(t *testing.T) {
	f, inbox, files := ideasTodoFixture(t)
	f.tracker.AddEpic("mw-lst", domain.Path{})
	f.tracker.DescribeEpic("mw-lst", "Discipleship Institute", apptest.StatusOpen, application.DefaultPriority)
	f.tracker.AddStory("mw-lst", domain.Story{ID: "mw-lst.1", Title: "Write the lesson plan"})
	mustDo(t, f.tracker.SetLabels("mw-lst.1", application.LabelHitl, application.LabelHitlTodo))
	f.message(t, releaseTapGovernorKey, "tx-todo-words", "helping me because I'm asking about to do")
	f.message(t, releaseTapGovernorKey, "tx-list-words", "can you help me with the instructions in my notes for my discipleship Institute")
	f.message(t, releaseTapGovernorKey, "tx-idea", "a new idea for Lampas")

	if _, err := inbox.Apply(context.Background()); err != nil {
		t.Fatalf("applying: %v", err)
	}
	if len(files.held) != 1 || files.held[0].Txid != "tx-idea" {
		t.Fatalf("expected only the idea held, got %+v", files.held)
	}
}

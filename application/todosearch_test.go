package application_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/todoindex"
)

// todoSearchBoard is a board with an open card, a plain open story and a card
// closed years ago whose comment holds an address, and the search over it with
// the real index file in a temp dir.
func todoSearchBoard(t *testing.T) (*apptest.FakeTracker, application.TodoSearch, *bytes.Buffer) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-todo", domain.Path{})
	add := func(id, title string, labels ...string) {
		tracker.AddStory("mw-todo", domain.Story{ID: id, Title: title})
		if err := tracker.SetLabels(id, labels...); err != nil {
			t.Fatal(err)
		}
	}
	add("mw-todo.1", "Buy a garden hose", application.LabelHitl, application.LabelHitlTodo)
	add("mw-todo.2", "Send the forwarding card", application.LabelHitl, application.LabelHitlTodo)
	add("mw-plain.1", "Quarry Road is not a To do card", application.LabelHitl)
	for _, comment := range []string{"ADDED by the Governor via postern", "New place: 14 Quarry Road, Portland, ME 04101"} {
		if err := tracker.CommentOnStory(context.Background(), "mw-todo.2", comment); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracker.CommentOnStory(context.Background(), "mw-plain.1", "Quarry Road again"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStatus("mw-todo.2", application.StatusClosed); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetClosedAt("mw-todo.2", time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	return tracker, application.TodoSearch{
		Tracker: tracker, Closed: tracker, Out: &out,
		Index: todoindex.Index{Path: filepath.Join(t.TempDir(), "state", "todo-search.db")},
	}, &out
}

func TestTodoSearchFindsAnAddressInACommentOfAClosedCard(t *testing.T) {
	tracker, search, out := todoSearchBoard(t)
	filed := tracker.Writes()
	printed, err := search.Run(context.Background(), []string{"quarry"})
	if err != nil {
		t.Fatal(err)
	}
	if printed != 1 {
		t.Fatalf("printed %d cards, want 1:\n%s", printed, out)
	}
	for _, want := range []string{"mw-todo.2", "closed", "Send the forwarding card", "comment:", "14 Quarry Road, Portland"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out.String(), "mw-plain.1") {
		t.Errorf("a story that is no To do card was found:\n%s", out)
	}
	if tracker.Writes() != filed {
		t.Errorf("the search wrote to the tracker %d times", tracker.Writes()-filed)
	}
}

func TestTodoSearchSaysNothingFoundAndSucceeds(t *testing.T) {
	_, search, out := todoSearchBoard(t)
	printed, err := search.Run(context.Background(), []string{"zzqxv"})
	if err != nil || printed != 0 {
		t.Fatalf("printed %d, err %v; want 0 and no error", printed, err)
	}
	if strings.TrimSpace(out.String()) != "nothing found" {
		t.Errorf("printed %q, want %q", out, "nothing found")
	}
}

func TestTodoSearchFindsOpenCardsByTitleAndNeedsEveryWord(t *testing.T) {
	_, search, out := todoSearchBoard(t)
	if printed, err := search.Run(context.Background(), []string{"GARDEN", "hose"}); err != nil || printed != 1 || !strings.Contains(out.String(), "mw-todo.1  open  Buy a garden hose") {
		t.Fatalf("printed %d, err %v:\n%s", printed, err, out)
	}
	out.Reset()
	if printed, err := search.Run(context.Background(), []string{"garden", "quarry"}); err != nil || printed != 0 {
		t.Fatalf("a card with only one of the words was found: printed %d, err %v:\n%s", printed, err, out)
	}
}

func TestTodoSearchBuildsTheIndexAnewOnEveryRun(t *testing.T) {
	tracker, search, _ := todoSearchBoard(t)
	if _, err := search.Run(context.Background(), []string{"quarry"}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.CommentOnStory(context.Background(), "mw-todo.2", "Also the Lantern Wharf shed"); err != nil {
		t.Fatal(err)
	}
	if printed, err := search.Run(context.Background(), []string{"lantern"}); err != nil || printed != 1 {
		t.Fatalf("a comment added after the first search was not found: printed %d, err %v", printed, err)
	}
}

func TestTodoSearchWithNoWordsIsRefused(t *testing.T) {
	_, search, _ := todoSearchBoard(t)
	if _, err := search.Run(context.Background(), []string{"  "}); err == nil {
		t.Fatal("no words was not refused")
	}
}

func TestTodoSearchTakesPunctuationAsText(t *testing.T) {
	_, search, _ := todoSearchBoard(t)
	for _, word := range []string{`"`, `quarry"`, `AND`, `NEAR(`, `*`, `04101,`} {
		if _, err := search.Run(context.Background(), []string{word}); err != nil {
			t.Errorf("%q: %v", word, err)
		}
	}
}

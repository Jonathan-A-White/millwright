package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// The kinds of field of a To do card the index holds a row for.
const (
	TodoFieldTitle       = "title"
	TodoFieldDescription = "description"
	TodoFieldComment     = "comment"
)

// TodoSearchLimit is how many cards mw todo search prints at most.
const TodoSearchLimit = 20

// todoSearchRows is how many rows of the index it reads for those cards: a card
// whose title, notes and comments all match has a row for each.
const todoSearchRows = 20 * TodoSearchLimit

// TodoDoc is one field of a To do card as the index holds it: its title, its
// description or one comment, with the card's id, status and title to show.
type TodoDoc struct {
	ID, Status, Title string
	Kind              string
	Body              string
}

// TodoHit is a row of the index that matched, with the line of it that did.
type TodoHit struct {
	ID, Status, Title string
	Kind              string
	Line              string
}

// TodoIndex is the full-text index of the To do cards, a cache of the tracker:
// the SQLite file of infrastructure/todoindex, kept in mw's state directory.
type TodoIndex interface {
	// Rebuild replaces everything the index holds with docs.
	Rebuild(ctx context.Context, docs []TodoDoc) error

	// Search lists the rows that hold every one of words, best match first, at
	// most limit.
	Search(ctx context.Context, words []string, limit int) ([]TodoHit, error)
}

// TodoSearch is mw todo search: it reads every card labelled hitl:todo, open or
// closed, with its comments; rebuilds the index from them; and prints the cards
// that hold the words. It only reads the tracker.
type TodoSearch struct {
	Tracker WorkTracker
	// Graph, when set, is read for the open cards that wait on an unfinished
	// story, which the tracker's ready list leaves out (openTodoCards).
	Graph  BeadGraph
	Closed ClosedTodoCards
	Index  TodoIndex
	Out    io.Writer
}

// todoSearchSince is the closing time the closed cards are read from: all of
// them.
var todoSearchSince = time.Unix(0, 0)

// Run rebuilds the index and prints the cards that match words, a line for each:
// id, status, title, then the matching line. It says "nothing found" when none
// does, which is not a failure. It returns how many cards it printed.
func (s TodoSearch) Run(ctx context.Context, words []string) (int, error) {
	if len(strings.Fields(strings.Join(words, " "))) == 0 {
		return 0, errors.New("mw todo search: give the words to look for")
	}
	if err := s.rebuild(ctx); err != nil {
		return 0, err
	}
	hits, err := s.Index.Search(ctx, words, todoSearchRows)
	if err != nil {
		return 0, err
	}
	shown := map[string]bool{}
	printed := 0
	for _, hit := range hits {
		if shown[hit.ID] || printed == TodoSearchLimit {
			continue
		}
		shown[hit.ID] = true
		printed++
		fmt.Fprintf(s.Out, "%s  %s  %s\n    %s: %s\n", hit.ID, hit.Status, hit.Title, hit.Kind, hit.Line)
	}
	if printed == 0 {
		fmt.Fprintln(s.Out, "nothing found")
	}
	return printed, nil
}

// rebuild reads the open cards and the closed ones, with their comments, and
// has the index replaced by them.
func (s TodoSearch) rebuild(ctx context.Context) error {
	open, err := openTodoCards(ctx, s.Tracker, s.Graph)
	if err != nil {
		return fmt.Errorf("reading the open To do cards: %w", err)
	}
	closed, err := s.Closed.ClosedWithLabel(ctx, LabelHitlTodo, todoSearchSince)
	if err != nil {
		return fmt.Errorf("reading the closed To do cards: %w", err)
	}
	var cards []StoryDetail
	var ids []string
	for _, card := range append(open, closed...) {
		if card.IsEpic || !card.Todo() {
			continue
		}
		cards = append(cards, card)
		ids = append(ids, card.Story.ID)
	}
	comments, err := s.Tracker.StoriesComments(ctx, ids)
	if err != nil {
		return fmt.Errorf("reading the comments of the To do cards: %w", err)
	}
	var docs []TodoDoc
	for _, card := range cards {
		doc := TodoDoc{ID: card.Story.ID, Status: card.Status, Title: card.Story.Title}
		for _, field := range []TodoDoc{
			{Kind: TodoFieldTitle, Body: card.Story.Title},
			{Kind: TodoFieldDescription, Body: card.Description},
		} {
			docs = appendTodoDoc(docs, doc, field.Kind, field.Body)
		}
		for _, comment := range comments[card.Story.ID] {
			docs = appendTodoDoc(docs, doc, TodoFieldComment, comment.Text)
		}
	}
	return s.Index.Rebuild(ctx, docs)
}

func appendTodoDoc(docs []TodoDoc, card TodoDoc, kind, body string) []TodoDoc {
	if strings.TrimSpace(body) == "" {
		return docs
	}
	card.Kind, card.Body = kind, body
	return append(docs, card)
}

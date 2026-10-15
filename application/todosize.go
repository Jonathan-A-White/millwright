package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// The metadata a hitl:todo card carries once it has a size and a Then line.
// todo_size is how big the card is, one of the TodoSizes; todo_then is free
// text, under TodoThenMax runes, naming what follows the card: when the
// Governor closes the card, the Then line is filed as a card of its own on the
// same list (fileThenCard). The Mayor sets them (mw todo size, mw todo then);
// todo_edit's size and then do the same from the Governor's phone.
const (
	TodoSizeField = "todo_size"
	TodoThenField = "todo_then"
)

// TodoSizes are the sizes a card can have, smallest first.
var TodoSizes = []string{"quick", "hour", "half-day"}

// TodoThenMax is the most runes a Then line holds.
const TodoThenMax = 200

// normalTodoSize is a size as it is kept: lower case, trimmed, and empty for
// none, which clears it. A size that is not one of TodoSizes is an error.
func normalTodoSize(text string) (string, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return "", nil
	}
	for _, size := range TodoSizes {
		if text == size {
			return size, nil
		}
	}
	return "", fmt.Errorf("%q is not a size: quick, hour or half-day", text)
}

// normalTodoThen is a Then line as it is kept: trimmed, and empty for none,
// which clears it. A line over TodoThenMax runes is an error.
func normalTodoThen(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n > TodoThenMax {
		return "", fmt.Errorf("the Then line is %d characters, over the limit of %d", n, TodoThenMax)
	}
	return text, nil
}

// setCardField writes one metadata field on the card id, once id is found to
// be an open hitl:todo card; every refusal comes before the write. command
// names the command in its errors.
func setCardField(ctx context.Context, tracker WorkTracker, command, id, field, value string) error {
	found, err := tracker.ShowBeads(ctx, []string{id})
	if err != nil {
		return fmt.Errorf("%s: reading %s: %w", command, id, err)
	}
	if len(found) == 0 {
		return fmt.Errorf("%s: %s is not a bead", command, id)
	}
	if why := refuseNotACard(found[0]); why != "" {
		return fmt.Errorf("%s: %s: %s", command, id, why)
	}
	if err := tracker.SetStoryMetadata(ctx, id, map[string]string{field: value}); err != nil {
		return fmt.Errorf("%s: %w", command, err)
	}
	return nil
}

// TodoSize sets or clears the size of a card: an open bead labelled hitl:todo.
type TodoSize struct {
	Tracker WorkTracker

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run gives the card id the size, or clears it when size is empty. It reports
// the size the card now has, "" for none.
func (t TodoSize) Run(ctx context.Context, id, size string) (string, error) {
	id = strings.TrimSpace(id)
	size, err := normalTodoSize(size)
	if err != nil {
		return "", fmt.Errorf("mw todo size: %w", err)
	}
	if err := setCardField(ctx, t.Tracker, "mw todo size", id, TodoSizeField, size); err != nil {
		return "", err
	}
	if t.Out != nil {
		if size == "" {
			fmt.Fprintf(t.Out, "%s: no size\n", id)
		} else {
			fmt.Fprintf(t.Out, "%s: %s\n", id, size)
		}
	}
	return size, nil
}

// TodoThen sets or clears the Then line of a card: an open bead labelled
// hitl:todo.
type TodoThen struct {
	Tracker WorkTracker

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run gives the card id the Then line, or clears it when text is empty. It
// reports the line the card now has, "" for none.
func (t TodoThen) Run(ctx context.Context, id, text string) (string, error) {
	id = strings.TrimSpace(id)
	then, err := normalTodoThen(text)
	if err != nil {
		return "", fmt.Errorf("mw todo then: %w", err)
	}
	if err := setCardField(ctx, t.Tracker, "mw todo then", id, TodoThenField, then); err != nil {
		return "", err
	}
	if t.Out != nil {
		if then == "" {
			fmt.Fprintf(t.Out, "%s: no Then line\n", id)
		} else {
			fmt.Fprintf(t.Out, "%s: then %s\n", id, then)
		}
	}
	return then, nil
}

// fileThenCard files the Then line of card, which has just been closed, as an
// open card of its own on the same list: labelled hitl and hitl:todo, titled
// the Then line, described "Follows <id>: <title>" and the card's Open: line
// if it has one. The new card's id is commented on the closed one. It reports
// the id, "" when the card has no Then line or is on no list.
func fileThenCard(ctx context.Context, tracker WorkTracker, card StoryDetail) (string, error) {
	then := strings.TrimSpace(card.TodoThen)
	if then == "" || card.IsEpic || !card.Todo() || card.EpicID == "" {
		return "", nil
	}
	description := fmt.Sprintf("Follows %s: %s", card.Story.ID, strings.TrimSpace(card.Story.Title))
	for _, line := range strings.Split(card.Description, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "Open:") {
			description += "\n" + line
			break
		}
	}
	id, err := tracker.CreateStory(ctx, NewStory{
		EpicID: card.EpicID, Title: then, Description: description, Priority: DefaultPriority, Labels: todoLabels,
	})
	if err != nil {
		return "", fmt.Errorf("filing the Then line %q failed: %w", then, err)
	}
	// CreateStory files held; a card is open the moment it is on the list.
	if err := tracker.ReleaseStory(ctx, id); err != nil {
		return "", fmt.Errorf("filed %s for the Then line but opening it failed: %w", id, err)
	}
	if err := tracker.CommentOnStory(ctx, card.Story.ID, fmt.Sprintf("THEN filed as %s: %s", id, then)); err != nil {
		return id, fmt.Errorf("filed %s for the Then line but commenting on %s failed: %w", id, card.Story.ID, err)
	}
	return id, nil
}

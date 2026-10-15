package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// todoSizeContext holds the fake work tracker and what came of the last
// mw todo size or mw todo then.
type todoSizeContext struct {
	tracker *apptest.FakeTracker

	printed bytes.Buffer
	err     error

	// writesBefore is how many writes the tracker had taken before the last
	// command.
	writesBefore int
}

// InitializeTodoSizeScenario registers the steps of features/todo-size.feature.
func InitializeTodoSizeScenario(ctx *godog.ScenarioContext) {
	c := &todoSizeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = todoSizeContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^the sized card "([^"]*)" is open$`, c.aCardIsOpen)
	ctx.Given(`^the sized bead "([^"]*)" is an open story with no hitl:todo label$`, c.aPlainStoryIsOpen)
	ctx.Given(`^the sized card "([^"]*)" is closed$`, c.theCardIsClosed)
	ctx.Given(`^the card "([^"]*)" is sized "([^"]*)"$`, c.theCardIsSized)
	ctx.Given(`^the card "([^"]*)" has the Then line "([^"]*)"$`, c.theCardHasTheThenLine)
	ctx.When(`^mw todo size is run on "([^"]*)" with "([^"]*)"$`, c.todoSizeIsRun)
	ctx.When(`^mw todo size is cleared on "([^"]*)"$`, func(id string) error { return c.todoSizeIsRun(id, "") })
	ctx.When(`^mw todo then is run on "([^"]*)" with "([^"]*)"$`, c.todoThenIsRun)
	ctx.When(`^mw todo then is run on "([^"]*)" with a line of (\d+) characters$`, func(id string, n int) error {
		return c.todoThenIsRun(id, strings.Repeat("x", n))
	})
	ctx.When(`^mw todo then is cleared on "([^"]*)"$`, func(id string) error { return c.todoThenIsRun(id, "") })

	ctx.Then(`^the card "([^"]*)" has the todo_size "([^"]*)"$`, c.hasTodoSize)
	ctx.Then(`^the card "([^"]*)" has no todo_size$`, func(id string) error { return c.hasTodoSize(id, "") })
	ctx.Then(`^the card "([^"]*)" has the todo_then "([^"]*)"$`, c.hasTodoThen)
	ctx.Then(`^the card "([^"]*)" has no todo_then$`, func(id string) error { return c.hasTodoThen(id, "") })
	ctx.Then(`^the todo (?:size|then) command succeeds$`, c.succeeds)
	ctx.Then(`^the todo (?:size|then) command is refused, saying: (.+)$`, c.isRefusedSaying)
	ctx.Then(`^nothing was written by the todo (?:size|then) command$`, c.nothingWritten)
}

func (c *todoSizeContext) aCardIsOpen(id string) error {
	c.tracker.AddEpic("mw-todo", domain.Path{})
	c.tracker.AddStory("mw-todo", domain.Story{ID: id, Title: "Card " + id})
	return c.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo)
}

func (c *todoSizeContext) aPlainStoryIsOpen(id string) error {
	c.tracker.AddStory("mw-todo", domain.Story{ID: id, Title: "Story " + id})
	return nil
}

func (c *todoSizeContext) theCardIsClosed(id string) error {
	return c.tracker.CloseStory(context.Background(), id, "done")
}

func (c *todoSizeContext) theCardIsSized(id, size string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoSizeField: size})
}

func (c *todoSizeContext) theCardHasTheThenLine(id, then string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoThenField: then})
}

func (c *todoSizeContext) todoSizeIsRun(id, size string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.TodoSize{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id, size)
	return nil
}

func (c *todoSizeContext) todoThenIsRun(id, text string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.TodoThen{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id, text)
	return nil
}

func (c *todoSizeContext) hasTodoSize(id, size string) error {
	if got := c.tracker.Metadata(id)[application.TodoSizeField]; got != size {
		return fmt.Errorf("expected %s to carry the todo_size %q, got %q", id, size, got)
	}
	return nil
}

func (c *todoSizeContext) hasTodoThen(id, then string) error {
	if got := c.tracker.Metadata(id)[application.TodoThenField]; got != then {
		return fmt.Errorf("expected %s to carry the todo_then %q, got %q", id, then, got)
	}
	return nil
}

func (c *todoSizeContext) succeeds() error {
	if c.err != nil {
		return fmt.Errorf("the command failed: %w", c.err)
	}
	return nil
}

func (c *todoSizeContext) isRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected the command to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *todoSizeContext) nothingWritten() error {
	if got := c.tracker.Writes() - c.writesBefore; got != 0 {
		return fmt.Errorf("expected nothing written, the tracker took %d writes", got)
	}
	return nil
}

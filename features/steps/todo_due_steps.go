package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// todoDueContext holds the fake work tracker and Postern sender, the clock the
// reminders read, and what came of the last mw todo due.
type todoDueContext struct {
	tracker *apptest.FakeTracker
	sender  *apptest.FakePosternSender
	now     time.Time

	printed bytes.Buffer
	err     error

	// runErr is what the last pass of the reminders failed with.
	runErr error

	// writesBefore is how many writes the tracker had taken before the last
	// mw todo due.
	writesBefore int
}

// InitializeTodoDueScenario registers the steps of features/todo_due.feature.
func InitializeTodoDueScenario(ctx *godog.ScenarioContext) {
	c := &todoDueContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = todoDueContext{tracker: apptest.NewFakeTracker(), sender: &apptest.FakePosternSender{}}
		return ctx, nil
	})

	ctx.Given(`^the to-do clock stands at "([^"]*)"$`, c.theClockStandsAt)
	ctx.When(`^the to-do clock stands at "([^"]*)"$`, c.theClockStandsAt)
	ctx.Given(`^the to-do card "([^"]*)" titled "([^"]*)" is open$`, c.aCardIsOpen)
	ctx.Given(`^the to-do bead "([^"]*)" is an open story with no hitl:todo label$`, c.aPlainStoryIsOpen)
	ctx.Given(`^the to-do card "([^"]*)" is closed$`, c.theCardIsClosed)
	ctx.Given(`^the card "([^"]*)" is due "([^"]*)"$`, c.theCardIsDue)
	ctx.When(`^the card "([^"]*)" is due "([^"]*)"$`, c.theCardIsDue)
	ctx.Given(`^the postern cannot be reached$`, func() error { c.sender.Err = errors.New("the postern backend is not reachable"); return nil })
	ctx.When(`^the postern can be reached$`, func() error { c.sender.Err = nil; return nil })
	ctx.When(`^the to-do reminders run$`, c.theRemindersRun)
	ctx.When(`^mw todo due is run on "([^"]*)" with "([^"]*)"$`, c.todoDueIsRun)
	ctx.When(`^mw todo due is cleared on "([^"]*)"$`, func(id string) error { return c.todoDueIsRun(id, "") })

	ctx.Then(`^exactly (\d+) reminders? w(?:as|ere) sent$`, c.remindersSent)
	ctx.Then(`^the to-do reminders failed, saying: (.+)$`, c.theRemindersFailed)
	ctx.Then(`^the reminder is an alarm threaded on "([^"]*)" and names "([^"]*)"$`, c.theReminderIsAnAlarm)
	ctx.Then(`^the card "([^"]*)" is marked reminded for "([^"]*)"$`, c.markedRemindedFor)
	ctx.Then(`^the card "([^"]*)" is not marked reminded$`, func(id string) error { return c.markedRemindedFor(id, "") })
	ctx.Then(`^the card "([^"]*)" has the todo_due "([^"]*)"$`, c.hasTodoDue)
	ctx.Then(`^the card "([^"]*)" has no todo_due$`, func(id string) error { return c.hasTodoDue(id, "") })
	ctx.Then(`^the todo due command succeeds$`, c.succeeds)
	ctx.Then(`^the todo due command is refused, saying: (.+)$`, c.isRefusedSaying)
	ctx.Then(`^nothing was written by the todo due command$`, c.nothingWritten)
}

func (c *todoDueContext) theClockStandsAt(at string) error {
	t, err := time.Parse(time.RFC3339, at)
	c.now = t
	return err
}

func (c *todoDueContext) aCardIsOpen(id, title string) error {
	c.tracker.AddEpic("mw-todo", domain.Path{})
	c.tracker.AddStory("mw-todo", domain.Story{ID: id, Title: title})
	return c.tracker.SetLabels(id, application.LabelHitl, application.LabelHitlTodo)
}

func (c *todoDueContext) aPlainStoryIsOpen(id string) error {
	c.tracker.AddStory("mw-todo", domain.Story{ID: id, Title: "Story " + id})
	return nil
}

func (c *todoDueContext) theCardIsClosed(id string) error {
	return c.tracker.CloseStory(context.Background(), id, "done")
}

func (c *todoDueContext) theCardIsDue(id, due string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.TodoDueField: due})
}

func (c *todoDueContext) theRemindersRun() error {
	_, c.runErr = application.TodoReminder{
		Tracker: c.tracker, Sender: c.sender, Now: func() time.Time { return c.now },
	}.Run(context.Background())
	return nil
}

func (c *todoDueContext) theRemindersFailed(reason string) error {
	if c.runErr == nil || !strings.Contains(c.runErr.Error(), reason) {
		return fmt.Errorf("expected the reminders to fail saying %q, got %v", reason, c.runErr)
	}
	return nil
}

func (c *todoDueContext) todoDueIsRun(id, when string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.TodoDue{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id, when)
	return nil
}

func (c *todoDueContext) remindersSent(want int) error {
	if c.runErr != nil && c.sender.Err == nil {
		return fmt.Errorf("the reminders failed: %w", c.runErr)
	}
	if got := len(c.sender.Sent()); got != want {
		return fmt.Errorf("expected %d reminders sent, got %d: %+v", want, got, c.sender.Sent())
	}
	return nil
}

func (c *todoDueContext) theReminderIsAnAlarm(bead, title string) error {
	sent := c.sender.Sent()
	if len(sent) == 0 {
		return fmt.Errorf("no reminder was sent")
	}
	req := sent[len(sent)-1]
	if req.Class != "alarm" {
		return fmt.Errorf("expected the class alarm, got %q", req.Class)
	}
	if req.Thread != bead {
		return fmt.Errorf("expected the reminder threaded on %s, got %q", bead, req.Thread)
	}
	if !strings.Contains(req.Text, title) {
		return fmt.Errorf("expected the reminder to name %q, it says %q", title, req.Text)
	}
	return nil
}

func (c *todoDueContext) markedRemindedFor(id, due string) error {
	if got := c.tracker.Metadata(id)[application.TodoRemindedField]; got != due {
		return fmt.Errorf("expected %s marked reminded for %q, got %q", id, due, got)
	}
	return nil
}

func (c *todoDueContext) hasTodoDue(id, due string) error {
	if got := c.tracker.Metadata(id)[application.TodoDueField]; got != due {
		return fmt.Errorf("expected %s to carry the todo_due %q, got %q", id, due, got)
	}
	return nil
}

func (c *todoDueContext) succeeds() error {
	if c.err != nil {
		return fmt.Errorf("mw todo due failed: %w", c.err)
	}
	return nil
}

func (c *todoDueContext) isRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected mw todo due to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *todoDueContext) nothingWritten() error {
	if got := c.tracker.Writes() - c.writesBefore; got != 0 {
		return fmt.Errorf("expected nothing written, the tracker took %d writes", got)
	}
	return nil
}

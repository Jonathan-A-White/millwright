package steps

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// askContext holds the fake tracker mw ask labels beads on, the clock it reads,
// and what the last ask printed or was refused with.
type askContext struct {
	tracker *apptest.FakeTracker
	now     time.Time
	out     bytes.Buffer
	err     error
}

// InitializeAskScenario registers the steps of features/ask.feature.
func InitializeAskScenario(ctx *godog.ScenarioContext) {
	c := &askContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = askContext{tracker: apptest.NewFakeTracker(), now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
		return ctx, nil
	})

	ctx.Given(`^the ask epic "([^"]*)" with the story "([^"]*)" titled "([^"]*)"$`, c.theAskEpicWithTheStory)
	ctx.Given(`^the ask clock reads "([^"]*)"$`, c.theAskClockReads)
	ctx.Given(`^the ask story "([^"]*)" is closed$`, c.theAskStoryIsClosed)
	ctx.Given(`^"([^"]*)" is marked waiting on "([^"]*)"$`, c.markedWaiting)
	ctx.When(`^"([^"]*)" is marked waiting on "([^"]*)"$`, c.markedWaiting)
	ctx.When(`^"([^"]*)" is marked done waiting$`, c.markedDone)
	ctx.When(`^the ask is recorded on "([^"]*)" as asked by "([^"]*)" in the role "([^"]*)"$`, c.askedBy)
	ctx.When(`^the ask is recorded on "([^"]*)" as asked of "([^"]*)" in the role "([^"]*)"$`, c.askedOf)
	ctx.Then(`^the ask succeeds$`, c.theAskSucceeds)
	ctx.Then(`^the ask is refused, saying: (.+)$`, c.theAskIsRefusedSaying)
	ctx.Then(`^the ask story "([^"]*)" carries the labels "([^"]*)"$`, c.theAskStoryCarries)
	ctx.Then(`^the ask story "([^"]*)" carries no labels$`, c.theAskStoryCarriesNone)
	ctx.Then(`^the ask story "([^"]*)" has been waiting since "([^"]*)"$`, c.theAskStoryWaitingSince)
	ctx.Then(`^the ask story "([^"]*)" has no waiting time$`, c.theAskStoryHasNoWaitingTime)
	ctx.Then(`^the ask says: (.+)$`, c.theAskSays)
}

func (c *askContext) ask() application.Ask {
	c.out.Reset()
	return application.Ask{Tracker: c.tracker, Notes: c.tracker, Now: func() time.Time { return c.now }, Out: &c.out}
}

func (c *askContext) theAskEpicWithTheStory(epic, id, title string) error {
	c.tracker.AddEpic(epic, domain.Path{})
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: title})
	return nil
}

func (c *askContext) theAskClockReads(at string) error {
	t, err := time.Parse(time.RFC3339, at)
	c.now = t
	return err
}

func (c *askContext) theAskStoryIsClosed(id string) error {
	return c.tracker.SetStatus(id, apptest.StatusClosed)
}

func (c *askContext) markedWaiting(id, of string) error {
	c.err = c.ask().Waiting(context.Background(), id, of, "")
	return nil
}

func (c *askContext) markedDone(id string) error {
	c.err = c.ask().Done(context.Background(), id)
	return nil
}

func (c *askContext) askedBy(id, login, role string) error {
	c.err = c.ask().By(context.Background(), id, login, role)
	return nil
}

func (c *askContext) askedOf(id, login, role string) error {
	c.err = c.ask().Of(context.Background(), id, login, role)
	return nil
}

func (c *askContext) theAskSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected the ask to succeed, got: %w", c.err)
	}
	return nil
}

func (c *askContext) theAskIsRefusedSaying(want string) error {
	if c.err == nil {
		return fmt.Errorf("expected the ask to be refused, it succeeded")
	}
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected the refusal to say %q, got %q", want, c.err)
	}
	return nil
}

func (c *askContext) labels(id string) ([]string, error) {
	d, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return nil, err
	}
	got := append([]string(nil), d.Labels...)
	sort.Strings(got)
	return got, nil
}

func (c *askContext) theAskStoryCarries(id, wantCSV string) error {
	got, err := c.labels(id)
	if err != nil {
		return err
	}
	var want []string
	if wantCSV != "" {
		want = splitLabels(wantCSV)
	}
	sort.Strings(want)
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		return fmt.Errorf("expected %s to carry %q, got %q", id, strings.Join(want, ", "), strings.Join(got, ", "))
	}
	return nil
}

func (c *askContext) theAskStoryCarriesNone(id string) error {
	return c.theAskStoryCarries(id, "")
}

func (c *askContext) waitingNote(id string) (string, error) {
	return c.tracker.Note(context.Background(), application.AskWaitingKey(id))
}

func (c *askContext) theAskStoryWaitingSince(id, want string) error {
	got, err := c.waitingNote(id)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected %s to be waiting since %q, the note says %q", id, want, got)
	}
	return nil
}

func (c *askContext) theAskStoryHasNoWaitingTime(id string) error {
	got, err := c.waitingNote(id)
	if err != nil {
		return err
	}
	if got != "" {
		return fmt.Errorf("expected no waiting time on %s, the note says %q", id, got)
	}
	return nil
}

func (c *askContext) theAskSays(want string) error {
	if got := strings.TrimSpace(c.out.String()); got != want {
		return fmt.Errorf("expected the ask to say %q, got %q", want, got)
	}
	return nil
}

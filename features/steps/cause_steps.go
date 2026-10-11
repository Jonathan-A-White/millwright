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

// causeContext holds the fake work tracker, the stories the scenario made and
// what came of naming a cause.
type causeContext struct {
	tracker *apptest.FakeTracker

	printed bytes.Buffer
	err     error

	// writesBefore is how many writes the tracker had taken before the last run.
	writesBefore int
}

// InitializeCauseScenario registers the steps of features/cause.feature.
func InitializeCauseScenario(ctx *godog.ScenarioContext) {
	c := &causeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = causeContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^the cause story "([^"]*)" is claimed and has been started (\d+) times$`, c.aStoryIsClaimed)
	ctx.Given(`^the cause story "([^"]*)" is open$`, c.aStoryIsOpen)
	ctx.When(`^mw records the cause of "([^"]*)" as (\S+), saying "([^"]*)"$`, c.recordsTheCause)
	ctx.When(`^mw records the cause of "([^"]*)" as (\S+), saying "([^"]*)", fixed by "([^"]*)"$`, c.recordsTheCauseFixedBy)
	ctx.Then(`^recording the cause succeeds$`, c.succeeds)
	ctx.Then(`^recording the cause is refused, saying: (.+)$`, c.isRefusedSaying)
	ctx.Then(`^nothing was written by the cause command$`, c.nothingWritten)
	ctx.Then(`^the story "([^"]*)" has the cause metadata "([^"]*)" as "([^"]*)"$`, c.hasMetadata)
	ctx.Then(`^the story "([^"]*)" has no cause metadata "([^"]*)"$`, c.hasNoMetadata)
	ctx.Then(`^the story "([^"]*)" has exactly (\d+) comments?, beginning "([^"]*)"$`, c.hasComments)
	ctx.Then(`^the cause comment on "([^"]*)" says "([^"]*)"$`, c.commentSays)
	ctx.Then(`^the story "([^"]*)" is still in progress, claimed, and started (\d+) times$`, c.stillClaimed)
	ctx.Then(`^the cause story "([^"]*)" has no metadata and no comment$`, c.untouched)
}

func (c *causeContext) aStoryIsClaimed(id string, started int) error {
	ctx := context.Background()
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	if err := c.tracker.ClaimStory(ctx, id); err != nil {
		return err
	}
	return c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.AttemptsField: fmt.Sprint(started)})
}

func (c *causeContext) aStoryIsOpen(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return nil
}

func (c *causeContext) run(id, class, why, fix string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.Cause{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id, class, why, fix)
	return nil
}

func (c *causeContext) recordsTheCause(id, class, why string) error {
	return c.run(id, class, why, "")
}

func (c *causeContext) recordsTheCauseFixedBy(id, class, why, fix string) error {
	return c.run(id, class, why, fix)
}

func (c *causeContext) succeeds() error {
	if c.err != nil {
		return fmt.Errorf("recording the cause failed: %w", c.err)
	}
	return nil
}

func (c *causeContext) isRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected recording the cause to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *causeContext) nothingWritten() error {
	if got := c.tracker.Writes() - c.writesBefore; got != 0 {
		return fmt.Errorf("expected nothing written, the tracker took %d writes", got)
	}
	return nil
}

func (c *causeContext) hasMetadata(id, key, want string) error {
	if got := c.tracker.Metadata(id)[key]; got != want {
		return fmt.Errorf("expected %s to carry %s=%q, it has %q", id, key, want, got)
	}
	return nil
}

func (c *causeContext) hasNoMetadata(id, key string) error {
	if got := c.tracker.Metadata(id)[key]; got != "" {
		return fmt.Errorf("expected %s to carry no %s, it has %q", id, key, got)
	}
	return nil
}

func (c *causeContext) hasComments(id string, want int, prefix string) error {
	comments := c.tracker.Comments(id)
	if len(comments) != want {
		return fmt.Errorf("expected %d comments on %s, found %d: %q", want, id, len(comments), comments)
	}
	for _, comment := range comments {
		if !strings.HasPrefix(comment, prefix) {
			return fmt.Errorf("expected every comment on %s to begin %q, got %q", id, prefix, comment)
		}
	}
	return nil
}

func (c *causeContext) commentSays(id, text string) error {
	comments := c.tracker.Comments(id)
	if len(comments) == 0 {
		return fmt.Errorf("%s has no comment", id)
	}
	if last := comments[len(comments)-1]; !strings.Contains(last, text) {
		return fmt.Errorf("expected the newest comment on %s to say %q, got %q", id, text, last)
	}
	return nil
}

func (c *causeContext) stillClaimed(id string, started int) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if detail.Status != apptest.StatusInProgress {
		return fmt.Errorf("expected %s still in progress, it is %q", id, detail.Status)
	}
	if detail.Assignee == "" {
		return fmt.Errorf("expected %s still claimed, it has no assignee", id)
	}
	if detail.Attempts != started {
		return fmt.Errorf("expected %s started %d times, it records %d", id, started, detail.Attempts)
	}
	return nil
}

func (c *causeContext) untouched(id string) error {
	if m := c.tracker.Metadata(id); len(m) > 0 {
		return fmt.Errorf("expected %s to carry no metadata, it has %v", id, m)
	}
	if comments := c.tracker.Comments(id); len(comments) > 0 {
		return fmt.Errorf("expected %s to carry no comment, it has %q", id, comments)
	}
	return nil
}

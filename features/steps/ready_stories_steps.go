package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// readyContext holds the fake work tracker under test, the epic most recently
// spoken about, and the stories the last listing returned.
type readyContext struct {
	tracker  *apptest.FakeTracker
	lastEpic string
	listed   []application.StoryDetail
	err      error
}

// InitializeReadyStoriesScenario registers the steps of
// features/ready_stories.feature.
func InitializeReadyStoriesScenario(ctx *godog.ScenarioContext) {
	c := &readyContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = readyContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^an epic "([^"]*)" with the default path:$`, c.anEpicWithTheDefaultPath)
	ctx.Given(`^a story "([^"]*)" of that epic with no overrides$`, c.aStoryOfThatEpicWithNoOverrides)
	ctx.Given(`^a story "([^"]*)" of that epic that overrides "([^"]*)" with "([^"]*)"$`, c.aStoryOfThatEpicThatOverrides)
	ctx.Given(`^the story "([^"]*)" is claimed$`, c.theStoryIsClaimed)
	ctx.Given(`^the story "([^"]*)" is closed because "([^"]*)"$`, c.theStoryIsClosedBecause)
	ctx.When(`^the ready stories of "([^"]*)" on "([^"]*)" are listed$`, c.theReadyStoriesAreListed)
	ctx.Then(`^the ready stories are "([^"]*)"$`, c.theReadyStoriesAre)
	ctx.Then(`^there are no ready stories$`, c.thereAreNoReadyStories)
	ctx.Then(`^the ready story "([^"]*)" is worked on model "([^"]*)"$`, c.theReadyStoryIsWorkedOnModel)
}

func (c *readyContext) anEpicWithTheDefaultPath(id string, table *godog.Table) error {
	defaults := domain.Path{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("default path rows need a field and a value, got %d cells", len(row.Cells))
		}
		if err := defaults.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	c.tracker.AddEpic(id, defaults)
	c.lastEpic = id
	return nil
}

func (c *readyContext) aStoryOfThatEpicWithNoOverrides(id string) error {
	c.tracker.AddStory(c.lastEpic, domain.Story{ID: id, Title: id})
	return nil
}

func (c *readyContext) aStoryOfThatEpicThatOverrides(id, field, value string) error {
	story := domain.Story{ID: id, Title: id}
	if err := story.Overrides.Set(field, value); err != nil {
		return err
	}
	c.tracker.AddStory(c.lastEpic, story)
	return nil
}

func (c *readyContext) theStoryIsClaimed(id string) error {
	return c.tracker.ClaimStory(context.Background(), id)
}

func (c *readyContext) theStoryIsClosedBecause(id, reason string) error {
	return c.tracker.CloseStory(context.Background(), id, reason)
}

func (c *readyContext) theReadyStoriesAreListed(epicID, host string) error {
	c.listed, c.err = c.tracker.ReadyStories(context.Background(), epicID, host)
	return nil
}

func (c *readyContext) theReadyStoriesAre(want string) error {
	if c.err != nil {
		return fmt.Errorf("listing the ready stories failed: %w", c.err)
	}
	got := make([]string, 0, len(c.listed))
	for _, d := range c.listed {
		got = append(got, d.Story.ID)
	}
	if strings.Join(got, ", ") != want {
		return fmt.Errorf("expected the ready stories to be %q, got %q", want, strings.Join(got, ", "))
	}
	return nil
}

func (c *readyContext) thereAreNoReadyStories() error {
	if c.err != nil {
		return fmt.Errorf("listing the ready stories failed: %w", c.err)
	}
	if len(c.listed) != 0 {
		return fmt.Errorf("expected no ready stories, got %d", len(c.listed))
	}
	return nil
}

func (c *readyContext) theReadyStoryIsWorkedOnModel(id, want string) error {
	for _, d := range c.listed {
		if d.Story.ID != id {
			continue
		}
		path, err := d.Path()
		if err != nil {
			return fmt.Errorf("the ready story %s has no path: %w", id, err)
		}
		if string(path.Model) != want {
			return fmt.Errorf("expected %s to be worked on model %q, got %q", id, want, path.Model)
		}
		return nil
	}
	return fmt.Errorf("%s is not among the ready stories", id)
}

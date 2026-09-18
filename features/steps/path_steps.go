package steps

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// pathContext holds the epic's defaults, the story under test and the result
// of building the story's path.
type pathContext struct {
	defaults domain.Path
	story    domain.Story
	built    domain.Path
	err      error
}

// InitializePathScenario registers the steps of features/path_validation.feature.
func InitializePathScenario(ctx *godog.ScenarioContext) {
	c := &pathContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = pathContext{}
		return ctx, nil
	})

	ctx.Given(`^an epic with the default path:$`, c.anEpicWithTheDefaultPath)
	ctx.Given(`^the epic's default (\w+) is not set$`, c.theEpicsDefaultFieldIsNotSet)
	ctx.Given(`^a story with no overrides$`, c.aStoryWithNoOverrides)
	ctx.Given(`^(?:a|the) story (?:that|also) overrides "([^"]*)" with "([^"]*)"$`, c.aStoryThatOverrides)
	ctx.When(`^the story's path is built$`, c.theStorysPathIsBuilt)
	ctx.Then(`^the path is rejected because: (.+)$`, c.thePathIsRejectedBecause)
	ctx.Then(`^the path is accepted$`, c.thePathIsAccepted)
	ctx.Then(`^the path's "([^"]*)" is "([^"]*)"$`, c.thePathsFieldIs)
}

func (c *pathContext) anEpicWithTheDefaultPath(table *godog.Table) error {
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("default path rows need a field and a value, got %d cells", len(row.Cells))
		}
		if err := c.defaults.Set(row.Cells[0].Value, row.Cells[1].Value); err != nil {
			return err
		}
	}
	return nil
}

func (c *pathContext) theEpicsDefaultFieldIsNotSet(field string) error {
	return c.defaults.Set(field, "")
}

func (c *pathContext) aStoryWithNoOverrides() error {
	c.story = domain.Story{ID: "mw-test", Title: "a story"}
	return nil
}

func (c *pathContext) aStoryThatOverrides(field, value string) error {
	if c.story.ID == "" {
		c.story = domain.Story{ID: "mw-test", Title: "a story"}
	}
	return c.story.Overrides.Set(field, value)
}

func (c *pathContext) theStorysPathIsBuilt() error {
	c.built, c.err = c.story.PathFrom(c.defaults)
	return nil
}

func (c *pathContext) thePathIsRejectedBecause(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected the path to be rejected because %q, but it was accepted: %+v", reason, c.built)
	}
	if c.err.Error() != reason {
		return fmt.Errorf("expected rejection %q, got %q", reason, c.err.Error())
	}
	return nil
}

func (c *pathContext) thePathIsAccepted() error {
	if c.err != nil {
		return fmt.Errorf("expected the path to be accepted, got %q", c.err.Error())
	}
	return nil
}

func (c *pathContext) thePathsFieldIs(field, want string) error {
	got, err := c.built.Field(field)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected the path's %s to be %q, got %q", field, want, got)
	}
	return nil
}

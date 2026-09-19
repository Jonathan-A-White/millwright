package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of features/show.feature. They run in the world of release.feature —
// the same plan, filed and held into the same fake tracker — because showing is
// the reading half of releasing, and the point is that both print one tree. They
// are registered from InitializeReleaseScenario.
func (c *releaseContext) registerShowSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^an epic "([^"]*)" called "([^"]*)" is filed under the epic of the filed plan$`, c.anEpicIsFiledUnderTheFiledEpic)
	ctx.When(`^the epic is shown$`, c.theEpicIsShown)
	ctx.When(`^the epic "([^"]*)" is shown$`, c.theNamedEpicIsShown)
	ctx.When(`^the story "([^"]*)" of the filed plan is shown as an epic$`, c.theStoryIsShownAsAnEpic)
	ctx.Then(`^showing succeeds$`, c.releasingSucceeds)
	ctx.Then(`^showing is refused, saying: (.+)$`, c.releasingIsRefusedSaying)
	ctx.Then(`^the shown tree names the epic (.+)$`, c.theReleaseTreeNamesTheEpic)
	ctx.Then(`^the shown tree shows the story "([^"]*)" on the path (.+)$`, c.theReleaseTreeShowsTheStoryOnThePath)
	ctx.Then(`^the shown tree shows the story "([^"]*)" waiting on "([^"]*)"$`, c.theReleaseTreeShowsTheStoryWaitingOn)
	ctx.Then(`^the shown tree shows the story "([^"]*)" waiting on nothing$`, c.theShownTreeShowsTheStoryWaitingOnNothing)
	ctx.Then(`^the shown tree shows the story "([^"]*)" as (.+)$`, c.theReleaseTreeShowsTheStoryAs)
	ctx.Then(`^the shown tree shows "([^"]*)" called "([^"]*)" as an epic, not a story$`, c.theShownTreeShowsAnEpicNotAStory)
	ctx.Then(`^nothing was written to the tracker$`, c.nothingWasWritten)
	ctx.Then(`^releasing the epic afterwards prints exactly the tree that was shown$`, c.releasingAfterwardsPrintsTheShownTree)
}

func (c *releaseContext) anEpicIsFiledUnderTheFiledEpic(id, title string) error {
	c.tracker.AddChildEpic(c.filed.EpicID, id, title)
	return nil
}

func (c *releaseContext) theEpicIsShown() error {
	return c.show(c.filed.EpicID)
}

func (c *releaseContext) theNamedEpicIsShown(epicID string) error {
	return c.show(epicID)
}

func (c *releaseContext) theStoryIsShownAsAnEpic(key string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	return c.show(id)
}

// show runs the use case, remembering how many writes the tracker had taken
// before it, so that a step can say the showing added none.
func (c *releaseContext) show(epicID string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.Show{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), epicID)
	return nil
}

func (c *releaseContext) nothingWasWritten() error {
	if got := c.tracker.Writes(); got != c.writesBefore {
		return fmt.Errorf("expected showing to write nothing, the tracker took %d writes", got-c.writesBefore)
	}
	return nil
}

func (c *releaseContext) theShownTreeShowsTheStoryWaitingOnNothing(key string) error {
	return c.treeBlockSays(key, "waits on nothing")
}

// theShownTreeShowsAnEpicNotAStory checks that a child epic is named as an epic,
// and that it does not carry what a story does — a path or an estimate.
func (c *releaseContext) theShownTreeShowsAnEpicNotAStory(id, title string) error {
	lines := strings.Split(c.printed.String(), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), id+" · "+title) {
			continue
		}
		block := strings.Join(lines[i:min(i+2, len(lines))], "\n")
		if !strings.Contains(block, "an epic") {
			return fmt.Errorf("expected the tree to say %s is an epic, got:\n%s", id, block)
		}
		if strings.Contains(block, "path ") || strings.Contains(block, "waits on") {
			return fmt.Errorf("expected the tree not to list the epic %s as a story, got:\n%s", id, block)
		}
		return nil
	}
	return fmt.Errorf("the tree does not show the epic %s · %s:\n%s", id, title, c.printed.String())
}

// releasingAfterwardsPrintsTheShownTree releases the epic that was just shown and
// checks that what it printed begins with exactly what the showing printed: the
// release adds its own line under the tree, and nothing before or inside it.
func (c *releaseContext) releasingAfterwardsPrintsTheShownTree() error {
	if err := c.releasingSucceeds(); err != nil {
		return err
	}
	shown := c.printed.String()
	if err := c.release(c.filed.EpicID); err != nil {
		return err
	}
	if c.err != nil {
		return fmt.Errorf("releasing the epic after showing it failed: %w", c.err)
	}
	if !strings.HasPrefix(c.printed.String(), shown) {
		return fmt.Errorf("expected releasing to print the tree that was shown:\n%s\nreleasing printed:\n%s", shown, c.printed.String())
	}
	return nil
}

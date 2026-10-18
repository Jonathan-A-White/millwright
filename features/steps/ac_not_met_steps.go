package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of features/ac_not_met.feature (mw-gq6.602): a landing whose closing
// comment says an acceptance criterion is not met. They hang off nextContext.

// registerAcNotMetSteps registers the steps of features/ac_not_met.feature.
func registerAcNotMetSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the story "([^"]*)" has the acceptance:$`, c.theStoryHasTheAcceptance)

	ctx.Then(`^exactly (\d+) bugs? (?:was|were) filed under "([^"]*)"$`, c.exactlyNBugsWereFiled)
	ctx.Then(`^the filed bug is titled "([^"]*)"$`, c.theFiledBugIsTitled)
	ctx.Then(`^the filed bug is held and labelled "([^"]*)" and "([^"]*)"$`, c.theFiledBugIsHeldAndLabelled)
	ctx.Then(`^the filed bug's description quotes: (.+)$`, c.theFiledBugsDescriptionQuotes)
	ctx.Then(`^the filed bug has the path of "([^"]*)"$`, c.theFiledBugHasThePathOf)
	ctx.Then(`^the filed bug's acceptance has a line beginning "([^"]*)"$`, c.theFiledBugsAcceptanceHasALine)
}

func (c *nextContext) theStoryHasTheAcceptance(id string, text *godog.DocString) error {
	return c.tracker.SetAcceptance(id, text.Content)
}

// filedBugs are the stories of the epic that mw next filed as bugs.
func (c *nextContext) filedBugs(epic string) ([]application.StoryDetail, error) {
	stories, err := c.tracker.StoriesSince(context.Background(), time.Time{})
	if err != nil {
		return nil, err
	}
	var bugs []application.StoryDetail
	for _, d := range stories {
		if d.EpicID == epic && strings.HasPrefix(d.Story.Title, "[bug] ") {
			bugs = append(bugs, d)
		}
	}
	return bugs, nil
}

func (c *nextContext) theFiledBug() (application.StoryDetail, error) {
	bugs, err := c.filedBugs("mw-gq6")
	if err != nil {
		return application.StoryDetail{}, err
	}
	if len(bugs) != 1 {
		return application.StoryDetail{}, fmt.Errorf("expected exactly one filed bug, got %d", len(bugs))
	}
	return bugs[0], nil
}

func (c *nextContext) exactlyNBugsWereFiled(n int, epic string) error {
	bugs, err := c.filedBugs(epic)
	if err != nil {
		return err
	}
	if len(bugs) != n {
		titles := make([]string, len(bugs))
		for i, b := range bugs {
			titles[i] = b.Story.Title
		}
		return fmt.Errorf("expected %d bugs filed under %s, got %d: %q", n, epic, len(bugs), titles)
	}
	return nil
}

func (c *nextContext) theFiledBugIsTitled(title string) error {
	bug, err := c.theFiledBug()
	if err != nil {
		return err
	}
	if bug.Story.Title != title {
		return fmt.Errorf("the filed bug is titled %q, expected %q", bug.Story.Title, title)
	}
	return nil
}

func (c *nextContext) theFiledBugIsHeldAndLabelled(first, second string) error {
	bug, err := c.theFiledBug()
	if err != nil {
		return err
	}
	if !bug.Held() {
		return fmt.Errorf("the filed bug %s is %q, expected it held", bug.Story.ID, bug.Status)
	}
	for _, want := range []string{first, second} {
		found := false
		for _, label := range bug.Labels {
			found = found || label == want
		}
		if !found {
			return fmt.Errorf("the filed bug %s has the labels %v, expected %q", bug.Story.ID, bug.Labels, want)
		}
	}
	return nil
}

func (c *nextContext) theFiledBugsDescriptionQuotes(quote string) error {
	bug, err := c.theFiledBug()
	if err != nil {
		return err
	}
	if !strings.Contains(bug.Description, quote) {
		return fmt.Errorf("the filed bug's description does not hold %q:\n%s", quote, bug.Description)
	}
	return nil
}

func (c *nextContext) theFiledBugHasThePathOf(id string) error {
	bug, err := c.theFiledBug()
	if err != nil {
		return err
	}
	landed, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if got, want := bug.Merged(), landed.Merged(); got != want {
		return fmt.Errorf("the filed bug's path is %+v, expected %+v", got, want)
	}
	return nil
}

func (c *nextContext) theFiledBugsAcceptanceHasALine(prefix string) error {
	bug, err := c.theFiledBug()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(bug.Acceptance, "\n") {
		if strings.HasPrefix(line, prefix) {
			return nil
		}
	}
	return fmt.Errorf("no line of the filed bug's acceptance begins %q:\n%s", prefix, bug.Acceptance)
}

package steps

import (
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// registerBatchLandTagSteps registers the steps of features/batch_land_tags.feature.
func registerBatchLandTagSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^"([^"]*)" is queued for batch landing, its branch tagged "([^"]*)"$`, c.aStoryIsQueuedTagged)
	ctx.Given(`^the rig has the tag "([^"]*)" on its first commit$`, c.theRigHasATagOnItsFirstCommit)
	ctx.Given(`^the rig's origin has the tag "([^"]*)" on its first commit$`, c.theOriginHasATagOnItsFirstCommit)
	ctx.Then(`^the tag "([^"]*)" is at the rig's origin on the commit of "([^"]*)"$`, c.theTagIsOnTheStoryCommit)
	ctx.Then(`^the tag "([^"]*)" is not at the rig's origin$`, c.theTagIsNotAtTheOrigin)
	ctx.Then(`^the tag "([^"]*)" at the rig's origin is still on its first commit$`, c.theTagIsStillOnTheFirstCommit)
}

// aStoryIsQueuedTagged is a story worked, tagged on its tip in its worktree as
// a Builder tags a release, and queued.
func (c *nextContext) aStoryIsQueuedTagged(id, tag string) error {
	if err := c.theStoryHasBeenWorked(id); err != nil {
		return err
	}
	if err := gitRun(application.WorktreeDir(c.rig, id), "git", "tag", "-f", tag); err != nil {
		return err
	}
	return c.queueByCloseOut(id)
}

func (c *nextContext) firstCommit() (string, error) {
	got, err := gitSay(c.rig, "rev-list", "--max-parents=0", "HEAD")
	return strings.TrimSpace(got), err
}

func (c *nextContext) theRigHasATagOnItsFirstCommit(tag string) error {
	first, err := c.firstCommit()
	if err != nil {
		return err
	}
	return gitRun(c.rig, "git", "tag", tag, first)
}

func (c *nextContext) theOriginHasATagOnItsFirstCommit(tag string) error {
	if err := c.theRigHasATagOnItsFirstCommit(tag); err != nil {
		return err
	}
	if err := gitRun(c.rig, "git", "push", "-q", "origin", "refs/tags/"+tag); err != nil {
		return err
	}
	return gitRun(c.rig, "git", "tag", "-d", tag)
}

func (c *nextContext) originTag(tag string) (string, error) {
	got, err := gitSay(c.origin(), "tag", "--list", tag)
	if err != nil || strings.TrimSpace(got) == "" {
		return "", err
	}
	return gitSay(c.origin(), "rev-parse", tag+"^{commit}")
}

func (c *nextContext) theTagIsOnTheStoryCommit(tag, id string) error {
	got, err := c.originTag(tag)
	if err != nil {
		return err
	}
	want, err := gitSay(c.origin(), "rev-parse", application.StoryBranch(id))
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) != strings.TrimSpace(want) {
		return fmt.Errorf("expected %s at the origin on the commit of %s (%s), got %q\n%s", tag, id, strings.TrimSpace(want), got, c.said())
	}
	return nil
}

func (c *nextContext) theTagIsNotAtTheOrigin(tag string) error {
	got, err := c.originTag(tag)
	if err != nil {
		return err
	}
	if got != "" {
		return fmt.Errorf("expected %s not at the origin, got it on %s", tag, got)
	}
	return nil
}

func (c *nextContext) theTagIsStillOnTheFirstCommit(tag string) error {
	first, err := c.firstCommit()
	if err != nil {
		return err
	}
	got, err := c.originTag(tag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(got) != first {
		return fmt.Errorf("expected %s at the origin left on %s, got %q", tag, first, got)
	}
	return nil
}

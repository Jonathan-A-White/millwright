package steps

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// mw-1knt69p: a grind example stopped on a host with no grist test key is
// pushed for the home to finish. features/grist_example_other_host.feature.

func registerNextGristExampleSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^this host holds no grist test key$`, func() error { return c.thisHostHoldsTheGristKey(false) })
	ctx.Given(`^this host holds the grist test key$`, func() error { return c.thisHostHoldsTheGristKey(true) })
	ctx.Given(`^the worktree and branch of "([^"]*)" are gone from this host$`, c.theWorktreeAndBranchAreGone)

	ctx.Then(`^the branch of "([^"]*)" is not on the rig's origin$`, c.theBranchIsNotOnTheOrigin)
	ctx.Then(`^the worktree of "([^"]*)" holds the grind example "([^"]*)", rebased onto "([^"]*)"$`, c.theWorktreeHoldsTheGrindExample)
}

func (c *nextContext) thisHostHoldsTheGristKey(held bool) error {
	c.gristKey = &held
	return nil
}

// hostGristKey is Next.HasGristTestKey for the scenario: nil unless it said.
func (c *nextContext) hostGristKey() func() bool {
	if c.gristKey == nil {
		return nil
	}
	held := *c.gristKey
	return func() bool { return held }
}

// theWorktreeAndBranchAreGone is the story as another host finds it: the first
// attempt's worktree and branch were never there, only the origin's copy is.
func (c *nextContext) theWorktreeAndBranchAreGone(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := gitRun(c.rig, "git", "worktree", "remove", "--force", dir); err != nil {
		return err
	}
	return gitRun(c.rig, "git", "branch", "-D", application.StoryBranch(id))
}

func (c *nextContext) theBranchIsNotOnTheOrigin(id string) error {
	if there, err := gitSay(c.origin(), "rev-parse", "--verify", "--quiet", "refs/heads/"+application.StoryBranch(id)); err == nil {
		return fmt.Errorf("expected %s not to be on the origin, but it is at %s", application.StoryBranch(id), there)
	}
	return nil
}

func (c *nextContext) theWorktreeHoldsTheGrindExample(id, file, branch string) error {
	return c.theWorktreeHoldsTheCommittedLeftover(id, file, branch)
}

package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

// The steps of what a landing does to this host's own checkout of the rig. They
// hang off nextContext: c.rig is the checkout mw next is handed, and the report
// they read is the one mwClosesOut printed.

// registerNextRigSteps registers the rig checkout steps of features/next.feature.
func registerNextRigSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig checkout has an uncommitted file "([^"]*)"$`, c.theRigCheckoutHasAnUncommittedFile)
	ctx.Given(`^the rig checkout is on a branch of its own, "([^"]*)"$`, c.theRigCheckoutIsOnABranch)

	ctx.Then(`^the rig checkout is at the commit that landed on "([^"]*)"$`, c.theRigCheckoutIsAtTheLandedCommit)
	ctx.Then(`^the report says the rig checkout was fast-forwarded$`, c.theReportSaysTheRigWasFastForwarded)
	ctx.Then(`^the rig checkout is left where it was, with "([^"]*)" untouched$`, c.theRigCheckoutIsLeftWithFileUntouched)
	ctx.Then(`^the rig checkout is left where it was, on "([^"]*)"$`, c.theRigCheckoutIsLeftOnBranch)
	ctx.Then(`^the report says the rig checkout was left, quoting: (.+)$`, c.theReportSaysTheRigWasLeft)
}

// rigNoteContent is what the uncommitted file holds, so that a step can tell it
// was not touched.
const rigNoteContent = "a note to self, not yet committed\n"

func (c *nextContext) theRigCheckoutHasAnUncommittedFile(name string) error {
	if err := os.WriteFile(filepath.Join(c.rig, name), []byte(rigNoteContent), 0o644); err != nil {
		return err
	}
	head, err := gitSay(c.rig, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	c.rigBefore = head
	return nil
}

func (c *nextContext) theRigCheckoutIsOnABranch(branch string) error {
	if err := gitRun(c.rig, "git", "checkout", "-q", "-b", branch); err != nil {
		return err
	}
	head, err := gitSay(c.rig, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	c.rigBefore = head
	return nil
}

func (c *nextContext) theRigCheckoutIsAtTheLandedCommit(branch string) error {
	landed, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	head, err := gitSay(c.rig, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != landed {
		return fmt.Errorf("expected the rig checkout at %s, the commit that landed on %s, got %s", landed, branch, head)
	}
	if _, err := os.Stat(filepath.Join(c.rig, "mw-gq6.1.md")); err != nil {
		return fmt.Errorf("expected the story's work in the rig checkout: %w", err)
	}
	return nil
}

func (c *nextContext) theReportSaysTheRigWasFastForwarded() error {
	if said := c.printed.String(); !strings.Contains(said, "fast-forwarded") {
		return fmt.Errorf("expected the report to say the rig checkout was fast-forwarded, got:\n%s", said)
	}
	return nil
}

func (c *nextContext) theRigCheckoutIsLeftWithFileUntouched(name string) error {
	head, err := gitSay(c.rig, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != c.rigBefore {
		return fmt.Errorf("expected the rig checkout left at %s, got %s", c.rigBefore, head)
	}
	got, err := os.ReadFile(filepath.Join(c.rig, name))
	if err != nil {
		return err
	}
	if string(got) != rigNoteContent {
		return fmt.Errorf("expected %s untouched, got %q", name, got)
	}
	return nil
}

func (c *nextContext) theRigCheckoutIsLeftOnBranch(branch string) error {
	on, err := gitSay(c.rig, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	head, err := gitSay(c.rig, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if on != branch || head != c.rigBefore {
		return fmt.Errorf("expected the rig checkout left on %s at %s, got %s at %s", branch, c.rigBefore, on, head)
	}
	return nil
}

func (c *nextContext) theReportSaysTheRigWasLeft(quoting string) error {
	said := c.printed.String()
	for _, want := range []string{"left as it was", quoting} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected the report to say the rig checkout was left, with %q in it, got:\n%s", want, said)
		}
	}
	return nil
}

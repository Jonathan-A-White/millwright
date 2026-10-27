package steps

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of a rig that lands its stories in batches (features/next_batch.feature,
// plans/0117): a close-out that queues the story rather than landing it. They hang
// off nextContext, whose close-out and check are the real ones.

// registerNextBatchSteps registers the batch-landing steps of features/next_batch.feature.
func registerNextBatchSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig lands its stories in batches$`, c.theRigLandsInBatches)
	ctx.Given(`^the rig lands its stories each by itself$`, c.theRigLandsEach)
	ctx.Given(`^the rig's origin already holds "([^"]*)" at a commit of an earlier attempt that the branch of "([^"]*)" does not contain$`, c.theOriginHoldsAnEarlierAttempt)
	ctx.Then(`^the origin's "([^"]*)" holds the earlier attempt's commit as well as the branch of "([^"]*)"$`, c.theOriginKeepsTheEarlierAttempt)
	ctx.Then(`^the story "([^"]*)" is recorded as queued for batch landing$`, c.theStoryIsRecordedAsQueued)
	ctx.Then(`^the branch of "([^"]*)" is on the rig's origin as "([^"]*)"$`, c.theBranchIsOnTheOriginAs)
	ctx.Then(`^the last ledger line says it was tested by mw check at the tip of "([^"]*)"$`, c.theLastLedgerLineTrustsTheCheck)
	ctx.Then(`^the last ledger line does not say "([^"]*)"$`, c.theLastLedgerLineDoesNotSay)
	ctx.Then(`^the close-out says the story is queued already$`, c.theCloseOutSaysQueuedAlready)
	ctx.Given(`^the rig's file is changed to landing = "batch" while the rig's tests run$`, c.theRigFileFlipsToBatchDuringTheTests)
	ctx.Then(`^the close-out never says "([^"]*)"$`, c.theCloseOutNeverSays)
	ctx.Then(`^the close-out says the rig's file was read again after the tests and said batch$`, c.theCloseOutSaysTheFileWasReadAgain)
}

func (c *nextContext) theRigLandsInBatches() error {
	return c.theRigFileHoldsTheLine(vault.LandingKey + ` = "batch"`)
}

func (c *nextContext) theRigLandsEach() error {
	return c.theRigFileHoldsTheLine(vault.LandingKey + ` = "each"`)
}

// theOriginHoldsAnEarlierAttempt puts a branch on the origin at a commit cut
// from main that the story's own branch, worked since, does not contain: what an
// earlier attempt's queue push left behind (mw-gq6.818).
func (c *nextContext) theOriginHoldsAnEarlierAttempt(name, _ string) error {
	commit, err := gitSay(c.origin(), "-c", "user.name=earlier attempt", "-c", "user.email=attempt@millwright.invalid",
		"commit-tree", "main^{tree}", "-p", "main", "-m", "An earlier attempt of the story")
	if err != nil {
		return err
	}
	return gitRun(c.origin(), "git", "update-ref", "refs/heads/"+name, commit)
}

// theOriginKeepsTheEarlierAttempt is the queued branch a fast-forward of the
// earlier attempt's push: its commit is an ancestor of the branch's tip.
func (c *nextContext) theOriginKeepsTheEarlierAttempt(name, id string) error {
	earlier, err := gitSay(c.origin(), "log", "-1", "--format=%H", "--grep=An earlier attempt of the story", "refs/heads/"+name)
	if err != nil {
		return err
	}
	if earlier == "" {
		return fmt.Errorf("expected %s at the origin to hold the earlier attempt's commit", name)
	}
	return gitRun(c.origin(), "git", "merge-base", "--is-ancestor", earlier, "refs/heads/"+name)
}

func (c *nextContext) theStoryIsRecordedAsQueued(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunQueued {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q (the close-out said: %v)\n%s",
			id, application.RunState, application.RunQueued, got, c.err, c.printed.String())
	}
	// A close-out refused because the story is queued already reports nothing.
	if c.err == nil && (!c.report.Queued || c.report.Landed) {
		return fmt.Errorf("expected the report to say %s was queued and not landed, got %+v", id, c.report)
	}
	return nil
}

// theBranchIsOnTheOriginAs is the story's branch at the origin under name,
// holding exactly the commit the story's branch is at here.
func (c *nextContext) theBranchIsOnTheOriginAs(id, name string) error {
	there, err := gitSay(c.origin(), "rev-parse", "refs/heads/"+name)
	if err != nil {
		return fmt.Errorf("expected %s on the origin: %w", name, err)
	}
	here, err := gitSay(c.rig, "rev-parse", application.StoryBranch(id))
	if err != nil {
		return err
	}
	if there != here {
		return fmt.Errorf("expected %s at the origin to be the tip of %s, %s, got %s", name, application.StoryBranch(id), here, there)
	}
	return nil
}

func (c *nextContext) lastLedgerLine() (string, error) {
	lines, err := c.ledgerLines()
	if err != nil {
		return "", err
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("the ledger is empty")
	}
	return lines[len(lines)-1], nil
}

func (c *nextContext) theLastLedgerLineTrustsTheCheck(id string) error {
	tip, err := gitSay(c.rig, "rev-parse", application.StoryBranch(id))
	if err != nil {
		return err
	}
	last, err := c.lastLedgerLine()
	if err != nil {
		return err
	}
	if want := "tested by mw check at " + tip[:12]; !strings.Contains(last, want) {
		return fmt.Errorf("expected the last ledger line to say %q, got:\n%s", want, last)
	}
	return nil
}

func (c *nextContext) theLastLedgerLineDoesNotSay(words string) error {
	last, err := c.lastLedgerLine()
	if err != nil {
		return err
	}
	if strings.Contains(last, words) {
		return fmt.Errorf("expected the last ledger line not to say %q, got:\n%s", words, last)
	}
	return nil
}

func (c *nextContext) theCloseOutSaysQueuedAlready() error {
	if c.err == nil || !strings.Contains(c.err.Error(), "queued for batch landing already") {
		return fmt.Errorf("expected the close-out to fail saying the story is queued for batch landing already, got %v", c.err)
	}
	return nil
}

// theRigFileFlipsToBatchDuringTheTests makes the rig's tests the moment the
// vault gains landing = "batch": the file said each when the close-out first
// read it, before the tests, as a vault the other host had not yet pushed to.
func (c *nextContext) theRigFileFlipsToBatchDuringTheTests() error {
	file := filepath.Join(c.vault, application.RigsDir, c.rigKey()+vault.RigFileExt)
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s; printf '%s = \"batch\"\\n' > %s", c.checkLog, vault.LandingKey, file)
	return nil
}

func (c *nextContext) theCloseOutNeverSays(words string) error {
	if strings.Contains(c.printed.String(), words) || c.err != nil && strings.Contains(c.err.Error(), words) {
		return fmt.Errorf("expected the close-out never to say %q, got %v\n%s", words, c.err, c.printed.String())
	}
	return nil
}

func (c *nextContext) theCloseOutSaysTheFileWasReadAgain() error {
	if want := "lands in batches now"; !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the report to say %q, got:\n%s", want, c.printed.String())
	}
	return nil
}

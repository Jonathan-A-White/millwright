package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of a landing that merges cleanly but whose merged result fails
// the rig's tests, and is sent back to a fresh Builder to fix them. They hang
// off nextContext, alongside the rebase send-back's own steps: the other
// host's change is made in c.seed, the fix a sent-back session would make is
// made for real in the story's worktree, and the sessions are the fake
// runner's.

// registerNextMergeFixSteps registers the send-back-for-tests steps of
// features/next.feature.
func registerNextMergeFixSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's tests fail only once "([^"]*)" is merged in$`, c.theRigsTestsFailOnlyWhenMerged)
	ctx.Given(`^the session sent back fixes the merged tests of "([^"]*)" and commits the fix$`, c.theSentBackSessionFixesTheMergedTests)
	ctx.Given(`^the session sent back for "([^"]*)" ends without fixing the merged tests$`, c.theSentBackSessionEndsWithoutFixingTheMergedTests)

	ctx.Then(`^a fresh session is running for "([^"]*)" in its worktree, told to fix the merged tests$`, c.aSessionIsRunningToFixTheMergedTests)
	ctx.Then(`^the story "([^"]*)" is recorded as sent back to fix the merged tests$`, c.theStoryIsRecordedAsSentBackForTests)
	ctx.Then(`^the report says "([^"]*)" was sent back to fix the merged tests$`, c.theReportSaysItWasSentBackForTests)
	ctx.Then(`^the worktree of "([^"]*)" already has "([^"]*)" merged in$`, c.theWorktreeAlreadyHasItMergedIn)
}

// theRigsTestsFailOnlyWhenMerged is a rig whose tests pass on either side's
// own work alone, but fail once the other host's file (nextOtherFile) is
// present too — a merge with no conflicts whose result still fails.
func (c *nextContext) theRigsTestsFailOnlyWhenMerged(_ string) error {
	c.checkCommand = fmt.Sprintf(
		"if [ -f %s ]; then printf 'run\\n' >> %s; echo 'FAIL: does not work together with the other host'\\''s change'; exit 1; fi; printf 'run\\n' >> %s",
		nextOtherFile, c.checkLog, c.checkLog)
	return nil
}

// theSentBackSessionFixesTheMergedTests is what the session sent back to fix
// the merged tests is told to do: find why the merged result fails, fix it,
// and commit the fix — leaving the rig's tests passing from here on.
func (c *nextContext) theSentBackSessionFixesTheMergedTests(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, "fix.md"), []byte("fixed to work with the other host's change\n"), 0o644); err != nil {
		return err
	}
	if err := gitRun(dir, "git", "add", "-A"); err != nil {
		return err
	}
	if err := gitRun(dir, "git", "commit", "-qm", "Fix the merged tests"); err != nil {
		return err
	}
	c.checkCommand = fmt.Sprintf("printf 'run\\n' >> %s", c.checkLog)
	return c.theSentBackSessionSucceeds(id)
}

func (c *nextContext) theSentBackSessionEndsWithoutFixingTheMergedTests(id string) error {
	return c.theSentBackSessionSucceeds(id)
}

func (c *nextContext) aSessionIsRunningToFixTheMergedTests(id string) error {
	name := application.SessionName(id)
	status, err := c.runner.Status(context.Background(), name)
	if err != nil {
		return err
	}
	if !status.Running() {
		return fmt.Errorf("expected a session %s running, got %+v (the close-out said: %v)", name, status, c.err)
	}
	spec, _ := c.runner.Spec(name)
	if want := application.WorktreeDir(c.rig, id); spec.Dir != want {
		return fmt.Errorf("expected the session %s to run in %s, got %q", name, want, spec.Dir)
	}
	said := strings.Join(spec.Command, " ")
	for _, want := range []string{"merged", "fix", "mw next"} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected the session %s to be told %q, got: %s", name, want, said)
		}
	}
	return nil
}

func (c *nextContext) theStoryIsRecordedAsSentBackForTests(id string) error {
	if got := c.tracker.State(id, application.MergedTestsState); got != application.MergedTestsSentBack {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.MergedTestsState, application.MergedTestsSentBack, got)
	}
	if got := c.tracker.State(id, application.RunState); got != application.RunRunning {
		return fmt.Errorf("expected %s to be recorded %s=%s again, got %q", id, application.RunState, application.RunRunning, got)
	}
	return nil
}

func (c *nextContext) theReportSaysItWasSentBackForTests(id string) error {
	if c.err != nil {
		return fmt.Errorf("expected a send-back to be no failure, got: %v", c.err)
	}
	if c.report.SentBack == "" {
		return fmt.Errorf("expected the report to name the session %s was sent back to, got %+v", id, c.report)
	}
	if want := "SENT BACK (merged-tests-fail) "; !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the printed report to say %q, got:\n%s", want, c.printed.String())
	}
	return nil
}

// theWorktreeAlreadyHasItMergedIn checks that the send-back merged branch into
// the story's own worktree before the fresh session was ever started, so that
// the fresh session finds the merge already there.
func (c *nextContext) theWorktreeAlreadyHasItMergedIn(id, branch string) error {
	dir := application.WorktreeDir(c.rig, id)
	if _, err := os.Stat(filepath.Join(dir, nextOtherFile)); err != nil {
		return fmt.Errorf("expected %s in the worktree %s to hold what %s carries, got: %v", nextOtherFile, dir, branch, err)
	}
	return nil
}

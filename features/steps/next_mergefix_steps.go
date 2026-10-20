package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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

	ctx.Given(`^the rig's tests fail, saying "([^"]*)", only once "([^"]*)" is merged in$`, c.theRigsTestsFailSayingOnlyWhenMerged)
	ctx.Given(`^the rig has a named-tests command that fails on main alone, saying "([^"]*)"$`, c.theNamedTestsFailOnMain)
	ctx.Given(`^the rig has a named-tests command that passes on main alone$`, c.theNamedTestsPassOnMain)
	ctx.Given(`^an open bug "([^"]*)" names the failing test "([^"]*)"$`, c.anOpenBugNamesTheTest)
	ctx.Then(`^the named-tests command was run once, given: (.+)$`, c.theNamedTestsRanOnce)
	ctx.Then(`^the named-tests command was not run$`, c.theNamedTestsDidNotRun)
	ctx.Then(`^the story "([^"]*)" is held on main's red, waiting on a new held bug naming "([^"]*)"$`, c.theStoryIsHeldOnAnewBug)
	ctx.Then(`^the story "([^"]*)" is held on main's red, waiting on the open bug "([^"]*)"$`, c.theStoryIsHeldOnTheBug)
	ctx.Then(`^the report says "([^"]*)" was held on main's red, not sent back$`, c.theReportSaysHeldOnMainsRed)
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
	if detail, err := c.tracker.ShowStory(context.Background(), id); err != nil {
		return err
	} else if detail.SentBackAt.IsZero() {
		return fmt.Errorf("expected %s to record when it was sent back (%s), which the quiet alarm counts from", id, application.SentBackAtField)
	}
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

// theRigsTestsFailSayingOnlyWhenMerged is theRigsTestsFailOnlyWhenMerged with
// the failure named as a Go test is, so that mw next can name the test it
// failed on.
func (c *nextContext) theRigsTestsFailSayingOnlyWhenMerged(saying, _ string) error {
	c.checkCommand = fmt.Sprintf(
		"if [ -f %s ]; then printf 'run\\n' >> %s; echo '%s'; exit 1; fi; printf 'run\\n' >> %s",
		nextOtherFile, c.checkLog, saying, c.checkLog)
	return nil
}

func (c *nextContext) namedLog() string { return filepath.Join(filepath.Dir(c.checkLog), "named.log") }

func (c *nextContext) setNamedCommand(command string) {
	if c.named == nil {
		c.named = map[string]string{}
	}
	c.named[c.rigKey()] = command
}

func (c *nextContext) theNamedTestsFailOnMain(saying string) error {
	c.setNamedCommand(fmt.Sprintf("set -- {tests}; printf 'run %%s\\n' \"$*\" >> %s; echo '%s'; exit 1", c.namedLog(), saying))
	return nil
}

func (c *nextContext) theNamedTestsPassOnMain() error {
	c.setNamedCommand(fmt.Sprintf("set -- {tests}; printf 'run %%s\\n' \"$*\" >> %s", c.namedLog()))
	return nil
}

func (c *nextContext) namedRuns() []string {
	held, err := os.ReadFile(c.namedLog())
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(held), "\n"), "\n")
}

func (c *nextContext) theNamedTestsRanOnce(given string) error {
	runs := c.namedRuns()
	if want := "run " + strings.TrimSpace(given); len(runs) != 1 || runs[0] != want {
		return fmt.Errorf("expected the named-tests command to have been run once, given %q, got %q", given, runs)
	}
	return nil
}

func (c *nextContext) theNamedTestsDidNotRun() error {
	if runs := c.namedRuns(); len(runs) != 0 {
		return fmt.Errorf("expected the named-tests command not to run, it ran: %q", runs)
	}
	return nil
}

// anOpenBugNamesTheTest files a released bug whose description names the test,
// as the Mayor's bug for main's red would.
func (c *nextContext) anOpenBugNamesTheTest(title, test string) error {
	id, err := c.tracker.CreateStory(context.Background(), application.NewStory{
		Standalone: true, Title: title, Labels: []string{application.LabelBug},
		Description: test + " fails on main by itself.",
	})
	if err != nil {
		return err
	}
	c.mainRedBug = id
	return c.tracker.ReleaseStory(context.Background(), id)
}

// mainRedBugs are the bugs mw next filed for main's red, found by title.
func (c *nextContext) mainRedBugs() ([]application.StoryDetail, error) {
	all, err := c.tracker.StoriesSince(context.Background(), time.Time{})
	if err != nil {
		return nil, err
	}
	var found []application.StoryDetail
	for _, d := range all {
		if strings.HasPrefix(d.Story.Title, "[bug] main is red: ") {
			found = append(found, d)
		}
	}
	return found, nil
}

// theStoryIsHeldOnMainsRed checks what a hold on main's red records on the story
// waiting on bug: the wait, the state, run=close-out-again, no send-back.
func (c *nextContext) theStoryIsHeldOnMainsRed(id, bug string) error {
	if c.err != nil {
		return fmt.Errorf("expected a hold to be no failure, got: %v", c.err)
	}
	beads, err := c.tracker.ShowBeads(context.Background(), []string{id})
	if err != nil || len(beads) != 1 {
		return fmt.Errorf("reading %s: %v (%d found)", id, err, len(beads))
	}
	if !slices.Contains(beads[0].Needs, bug) {
		return fmt.Errorf("expected %s to wait on %s, it waits on %v", id, bug, beads[0].Needs)
	}
	if got := c.tracker.State(id, application.MainRedState); got != bug {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.MainRedState, bug, got)
	}
	if got := c.tracker.State(id, application.RunState); got != application.RunCloseOutAgain {
		return fmt.Errorf("expected %s to be recorded %s=%s, got %q", id, application.RunState, application.RunCloseOutAgain, got)
	}
	if got := c.tracker.State(id, application.MergedTestsState); got != "" {
		return fmt.Errorf("expected %s not to be sent back to fix the merged tests, got %s=%q", id, application.MergedTestsState, got)
	}
	return nil
}

func (c *nextContext) theStoryIsHeldOnAnewBug(id, test string) error {
	bugs, err := c.mainRedBugs()
	if err != nil {
		return err
	}
	if len(bugs) != 1 || !strings.Contains(bugs[0].Story.Title+bugs[0].Description, test) {
		return fmt.Errorf("expected one new [bug] naming %s, got %+v", test, bugs)
	}
	if bugs[0].Status != "deferred" && bugs[0].Status != "held" {
		return fmt.Errorf("expected the new bug %s to be held, its status is %q", bugs[0].Story.ID, bugs[0].Status)
	}
	return c.theStoryIsHeldOnMainsRed(id, bugs[0].Story.ID)
}

func (c *nextContext) theStoryIsHeldOnTheBug(id, _ string) error {
	if bugs, err := c.mainRedBugs(); err != nil {
		return err
	} else if len(bugs) != 0 {
		return fmt.Errorf("expected no new bug, mw next filed %+v", bugs)
	}
	return c.theStoryIsHeldOnMainsRed(id, c.mainRedBug)
}

func (c *nextContext) theReportSaysHeldOnMainsRed(id string) error {
	if c.report.MainRed == nil || c.report.SentBack != "" || c.report.Refused {
		return fmt.Errorf("expected the report to say %s was held on main's red, not sent back or refused, got %+v", id, c.report)
	}
	if want := "HELD (main-red) main is red: "; !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the printed report to say %q, got:\n%s", want, c.printed.String())
	}
	return nil
}

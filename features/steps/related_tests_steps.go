package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of a rig's related-tests line (features/related_tests.feature): the
// line is the rig's, given the files the story's branch changed, and it logs
// what it was given so that a scenario can say which files it was run on, or
// that it was not run at all. Only the landing gate is given the line; mw check
// is built without it.

// registerRelatedTestsSteps registers the related-tests steps.
func registerRelatedTestsSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig has a related-tests command that logs the files it is given$`, c.theRigLogsRelatedFiles)
	ctx.Given(`^the rig has a related-tests command that fails, saying "([^"]*)"$`, c.theRigsRelatedTestsFail)
	ctx.Given(`^the story "([^"]*)" changed nothing but deleting the file "([^"]*)"$`, c.theStoryOnlyDeleted)

	ctx.Then(`^the related-tests command was run once, given: (.+)$`, c.theRelatedTestsRanOnce)
	ctx.Then(`^the related-tests command was run (\d+) times, each given: (.+)$`, c.theRelatedTestsRanEach)
	ctx.Then(`^the related-tests command was last given: (.+)$`, c.theRelatedTestsWereLastGiven)
	ctx.Then(`^the related-tests command was not run$`, c.theRelatedTestsDidNotRun)
	ctx.Then(`^mw said on stderr: (.+)$`, c.mwSaidOnStderr)
}

// relatedLog is where a scenario's related-tests command writes what it was given.
func (c *nextContext) relatedLog() string {
	return filepath.Join(filepath.Dir(c.checkLog), "related.log")
}

func (c *nextContext) setRelatedCommand(command string) {
	if c.related == nil {
		c.related = map[string]string{}
	}
	c.related[c.rigKey()] = command
}

func (c *nextContext) theRigLogsRelatedFiles() error {
	c.setRelatedCommand(fmt.Sprintf("set -- {files}; printf 'run %%s\\n' \"$*\" >> %s", c.relatedLog()))
	return nil
}

func (c *nextContext) theRigsRelatedTestsFail(saying string) error {
	c.setRelatedCommand(fmt.Sprintf("set -- {files}; printf 'run %%s\\n' \"$*\" >> %s; echo '%s'; exit 1", c.relatedLog(), saying))
	return nil
}

// theStoryOnlyDeleted leaves the story's branch with one commit more that takes
// away what the story added and the file given, so that the branch's net change
// against its merge-base is a deletion and nothing else.
func (c *nextContext) theStoryOnlyDeleted(id, file string) error {
	dir := application.WorktreeDir(c.rig, id)
	for _, args := range [][]string{{"rm", "-q", id + ".md", file}, {"commit", "-qm", "Deletes " + file}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) relatedRuns() []string {
	held, err := os.ReadFile(c.relatedLog())
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(held), "\n"), "\n")
}

func (c *nextContext) theRelatedTestsRanOnce(given string) error {
	return c.theRelatedTestsRanEach(1, given)
}

func (c *nextContext) theRelatedTestsRanEach(times int, given string) error {
	runs := c.relatedRuns()
	want := "run " + strings.TrimSpace(given)
	if len(runs) != times {
		return fmt.Errorf("expected the related-tests command to have been run %d times, got %d: %q", times, len(runs), runs)
	}
	for _, run := range runs {
		if run != want {
			return fmt.Errorf("expected the related-tests command to have been given %q, got %q", given, strings.TrimPrefix(run, "run "))
		}
	}
	return nil
}

func (c *nextContext) theRelatedTestsWereLastGiven(given string) error {
	runs := c.relatedRuns()
	want := "run " + strings.TrimSpace(given)
	if len(runs) == 0 || runs[len(runs)-1] != want {
		return fmt.Errorf("expected the related-tests command to have last been given %q, it ran: %q", given, runs)
	}
	return nil
}

func (c *nextContext) theRelatedTestsDidNotRun() error {
	if runs := c.relatedRuns(); len(runs) > 0 {
		return fmt.Errorf("expected the related-tests command not to have run, but it logged: %q", runs)
	}
	return nil
}

func (c *nextContext) mwSaidOnStderr(text string) error {
	if !strings.Contains(c.stderr.String(), text) {
		return fmt.Errorf("expected mw to say %q on stderr, it said:\n%s", text, c.stderr.String())
	}
	return nil
}

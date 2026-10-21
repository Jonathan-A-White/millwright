package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of a rig's changed-specs command, shared by features/next.feature
// and features/check.feature: the command is the rig's, given the specs the
// story's branch added or changed, and it logs what it was given so that a
// scenario can say which specs it was run on, or that it was not run at all.

// registerNextSpecsSteps registers the changed-specs steps.
func registerNextSpecsSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig has a changed-specs command that logs the specs it is given$`, c.theRigLogsChangedSpecs)
	ctx.Given(`^the rig has a changed-specs command that fails, saying "([^"]*)"$`, c.theRigsChangedSpecsFail)
	ctx.Given(`^the rig has a changed-specs command that fails only once "([^"]*)" is merged in$`, c.theRigsChangedSpecsFailOnlyWhenMerged)
	ctx.Given(`^the rig's timing_tests name the spec "([^"]*)"$`, c.theRigsTimingTestsNameTheSpec)
	ctx.Given(`^the story "([^"]*)" also added the spec "([^"]*)"$`, c.theStoryAddedAFile)
	ctx.Given(`^the story "([^"]*)" also added the file "([^"]*)"$`, c.theStoryAddedAFile)

	ctx.Then(`^the changed-specs command was run once, given: (.+)$`, c.theChangedSpecsRanOnce)
	ctx.Then(`^the changed-specs command was not run$`, c.theChangedSpecsDidNotRun)
}

// specLog is where a scenario's changed-specs command writes what it was given.
func (c *nextContext) specLog() string {
	return filepath.Join(filepath.Dir(c.checkLog), "specs.log")
}

func (c *nextContext) setSpecCommand(command string) {
	if c.specCommands == nil {
		c.specCommands = map[string]string{}
	}
	c.specCommands[c.rigKey()] = command
}

func (c *nextContext) theRigsTimingTestsNameTheSpec(spec string) error {
	if c.timingTests == nil {
		c.timingTests = map[string][]string{}
	}
	c.timingTests[c.rigKey()] = append(c.timingTests[c.rigKey()], spec)
	return nil
}

func (c *nextContext) theRigLogsChangedSpecs() error {
	c.setSpecCommand(fmt.Sprintf("printf 'run %%s\\n' {specs} >> %s", c.specLog()))
	return nil
}

func (c *nextContext) theRigsChangedSpecsFail(saying string) error {
	c.setSpecCommand(fmt.Sprintf("printf 'run %%s\\n' {specs} >> %s; echo '%s'; exit 1", c.specLog(), saying))
	return nil
}

func (c *nextContext) theRigsChangedSpecsFailOnlyWhenMerged(_ string) error {
	c.setSpecCommand(fmt.Sprintf(
		"if [ -f %s ]; then echo '1 failed once the other host'\\''s work is in'; exit 1; fi; printf 'run %%s\\n' {specs} >> %s",
		nextOtherFile, c.specLog()))
	return nil
}

// theStoryAddedAFile commits one more file, at the path given, in the story's
// worktree.
func (c *nextContext) theStoryAddedAFile(id, file string) error {
	dir := application.WorktreeDir(c.rig, id)
	full := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, []byte("added by "+id+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "Adds " + file}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theChangedSpecsRanOnce(given string) error {
	held, err := os.ReadFile(c.specLog())
	if err != nil {
		return fmt.Errorf("expected the changed-specs command to have run, but it left no log: %w", err)
	}
	want := "run " + strings.TrimSpace(given) + "\n"
	if string(held) != want {
		return fmt.Errorf("expected the changed-specs command to have been run once, given %q, got:\n%s", given, held)
	}
	return nil
}

func (c *nextContext) theChangedSpecsDidNotRun() error {
	if held, err := os.ReadFile(c.specLog()); err == nil {
		return fmt.Errorf("expected the changed-specs command not to have run, but it logged:\n%s", held)
	}
	return nil
}

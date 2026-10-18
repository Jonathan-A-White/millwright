package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// registerCheckPartsSteps registers the steps of features/check_parts.feature: a
// rig whose gate the host names as parts, each part a shell command that writes
// one word to the scenario's log so that what ran, and in what order, can be read.
func registerCheckPartsSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's gate is split into these parts:$`, c.theGateIsSplit)
	ctx.Given(`^the rig's gate is not split into parts$`, c.theGateIsNotSplit)
	ctx.Given(`^the part "([^"]*)" fails, saying "([^"]*)"$`, c.aPartFails)

	ctx.When(`^a session checks "([^"]*)" part (\d+)$`, c.aSessionChecksAPart)
	ctx.When(`^the session of "([^"]*)" commits another change$`, c.theSessionCommitsAgain)

	ctx.Then(`^the check part passes$`, c.theCheckPartPasses)
	ctx.Then(`^the check part fails$`, c.theCheckPartFails)
	ctx.Then(`^the check part error says: (.+)$`, c.theCheckPartErrorSays)
	ctx.Then(`^the parts that ran, in order, were: (.+)$`, c.thePartsThatRan)
}

func (c *nextContext) theGateIsSplit(table *godog.Table) error {
	c.gateParts = nil
	for _, row := range table.Rows[1:] {
		command := strings.ReplaceAll(row.Cells[1].Value, "{log}", c.checkLog)
		c.gateParts = append(c.gateParts, application.GatePart{Name: row.Cells[0].Value, Command: command})
	}
	return nil
}

func (c *nextContext) theGateIsNotSplit() error {
	c.gateParts = nil
	return nil
}

func (c *nextContext) aPartFails(name, saying string) error {
	for i, part := range c.gateParts {
		if part.Name == name {
			c.gateParts[i].Command = fmt.Sprintf("%s; echo '%s'; exit 1", part.Command, saying)
			return nil
		}
	}
	return fmt.Errorf("the gate has no part %q", name)
}

func (c *nextContext) aSessionChecksAPart(id string, part int) error {
	c.partToCheck = part
	defer func() { c.partToCheck = 0 }()
	return c.checkWith(id, rig.NewSlots())
}

func (c *nextContext) theSessionCommitsAgain(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.WriteFile(filepath.Join(dir, id+"-again.md"), []byte("more work on "+id+"\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "More work on " + id}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theCheckPartPasses() error {
	if c.err != nil {
		return fmt.Errorf("expected the part to pass, got %v\n%s", c.err, c.printed.String())
	}
	return nil
}

func (c *nextContext) theCheckPartFails() error { return c.theCheckFails() }

func (c *nextContext) theCheckPartErrorSays(words string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), strings.TrimSpace(words)) {
		return fmt.Errorf("expected the error to say %q, got %v", words, c.err)
	}
	return nil
}

func (c *nextContext) thePartsThatRan(words string) error {
	var ran []string
	if held, err := os.ReadFile(c.checkLog); err == nil {
		ran = strings.Fields(string(held))
	}
	if got, want := strings.Join(ran, ", "), strings.TrimSpace(words); got != want {
		return fmt.Errorf("expected the parts %q to have run, got %q", want, got)
	}
	return nil
}

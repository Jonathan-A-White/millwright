package steps

import (
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of features/next_running.feature run the close-out of next.feature's
// world with a heartbeat process alive, or ended, beside the story's session.

// registerNextRunningSteps registers them with nextContext.
func registerNextRunningSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the session of "([^"]*)" is still running, its heartbeat alive as process (\d+)$`, c.theHeartbeatIsAlive)
	ctx.Then(`^the close-out refuses, naming the running session of "([^"]*)"$`, c.theCloseOutRefusesNamingTheSession)
	ctx.Then(`^the ledger holds only the lines it held before$`, c.theLedgerHoldsOnlyWhatItHeld)
	ctx.Then(`^the story "([^"]*)" is still running and carries no comment$`, c.theStoryIsStillRunningUncommented)
}

func (c *nextContext) theHeartbeatIsAlive(id string, pid int) error {
	c.beating.Start(id, pid)
	return nil
}

func (c *nextContext) theCloseOutRefusesNamingTheSession(id string) error {
	if c.err == nil {
		return fmt.Errorf("expected the close-out to refuse, it reported no failure (report %+v)", c.report)
	}
	for _, want := range []string{application.SessionName(id), "still running"} {
		if !strings.Contains(c.err.Error(), want) {
			return fmt.Errorf("expected the refusal to say %q, got %q", want, c.err)
		}
	}
	return nil
}

func (c *nextContext) theLedgerHoldsOnlyWhatItHeld() error {
	lines, err := c.ledgerLines()
	if err != nil {
		return err
	}
	if len(lines) != len(c.ledgerBefore) {
		return fmt.Errorf("expected the ledger to hold its %d lines, got %d: %q", len(c.ledgerBefore), len(lines), lines)
	}
	return nil
}

func (c *nextContext) theStoryIsStillRunningUncommented(id string) error {
	if got := c.tracker.State(id, application.RunState); got != application.RunRunning {
		return fmt.Errorf("expected %s to stay %s=%s, got %q", id, application.RunState, application.RunRunning, got)
	}
	if comments := c.tracker.Comments(id); len(comments) != 0 {
		return fmt.Errorf("expected %s to carry no comment, got %q", id, comments)
	}
	return nil
}

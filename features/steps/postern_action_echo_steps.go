package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// registerPosternActionEchoSteps adds the steps of
// features/postern_action_echo.feature to the inbox's world.
func (c *posternInboxContext) registerPosternActionEchoSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the home's event log is kept$`, func() error {
		c.eventLog = &apptest.FakeEventLog{}
		return nil
	})
	ctx.Then(`^the events tail shows an action_applied event on "([^"]*)" with detail "([^"]*)"$`, c.theEventsTailShowsAnActionApplied)
	ctx.Then(`^the events tail shows no events$`, c.theEventsTailShowsNoEvents)
}

// eventsTail is what mw events tail prints for the whole log, line by line.
func (c *posternInboxContext) eventsTail() ([]string, error) {
	var out bytes.Buffer
	if err := (application.EventTail{Log: c.eventLog, Out: &out}).Run(context.Background()); err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(out.String()), "\n"), nil
}

func (c *posternInboxContext) theEventsTailShowsAnActionApplied(bead, txid string) error {
	lines, err := c.eventsTail()
	if err != nil {
		return err
	}
	var matched []string
	for _, line := range lines {
		// seq, time, kind, bead, actor, move, detail
		fields := strings.Fields(line)
		if len(fields) == 7 && fields[2] == "action_applied" && fields[3] == bead && fields[6] == txid {
			matched = append(matched, line)
		}
	}
	if len(matched) != 1 {
		return fmt.Errorf("the events tail has %d action_applied lines for %s with detail %s, want 1:\n%s", len(matched), bead, txid, strings.Join(lines, "\n"))
	}
	return nil
}

func (c *posternInboxContext) theEventsTailShowsNoEvents() error {
	lines, err := c.eventsTail()
	if err != nil {
		return err
	}
	if len(lines) != 1 || lines[0] != "" {
		return fmt.Errorf("the events tail is not empty:\n%s", strings.Join(lines, "\n"))
	}
	return nil
}

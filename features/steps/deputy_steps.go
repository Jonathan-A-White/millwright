package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/cucumber/godog"
)

// The steps of features/deputy.feature. Like mw millhand, mw deputy is a thin
// command over mw seat up, so its scenarios run in the seat up context: the
// same vault on disk, the same faked terminal, and the config file of
// millhand_steps.go for the model.

// registerDeputySteps adds the steps of features/deputy.feature to the seat up
// scenario.
func registerDeputySteps(ctx *godog.ScenarioContext, c *seatUpContext) {
	ctx.Given(`^the Deputy's box holds (\d+) unread messages$`, c.theDeputysBoxHolds)
	ctx.Given(`^the pane of the window "([^"]*)" is busy$`, c.thePaneIsBusy)
	ctx.Given(`^the window "([^"]*)" loses the first Enter key it is sent$`, c.theWindowLosesTheFirstEnter)
	ctx.Given(`^the window "([^"]*)" loses every Enter key it is sent$`, c.theWindowLosesEveryEnter)

	ctx.When(`^mw deputy is run$`, c.mwDeputyIsRun)
	ctx.When(`^mw deputy is run for the reason "([^"]*)"$`, c.mwDeputyIsRunFor)

	ctx.Then(`^mw deputy succeeds$`, c.seatUpSucceeds)
	ctx.Then(`^mw deputy is refused saying the Deputy is already up in "([^"]*)"$`, c.deputyRefusedForBeingUp)
	ctx.Then(`^mw deputy is refused saying there is no charter$`, c.refusedForNoCharter)
	ctx.Then(`^mw deputy is refused saying the Deputy is busy in "([^"]*)" and the mail waits$`, c.deputyRefusedForBeingBusy)
	ctx.Then(`^mw deputy says it pressed Enter again$`, c.deputySaysEnterAgain)
	ctx.Then(`^mw deputy says the nudge is on its input line still$`, c.deputySaysStuck)
	ctx.Then(`^mw deputy says it nudged the Deputy in the window "([^"]*)"$`, c.deputySaysItNudged)
	ctx.Then(`^the line "([^"]*)" was typed into the window "([^"]*)"$`, c.theLineWasTyped)
	ctx.Then(`^nothing was typed into the window "([^"]*)"$`, c.nothingWasTyped)
	ctx.Then(`^the Enter key was pressed (\d+) times in the window "([^"]*)"$`, c.enterWasPressed)
	ctx.Then(`^mw deputy leaves with the status (\d+)$`, c.millhandLeavesWith)
}

func (c *seatUpContext) mwDeputyIsRun() error {
	return c.bringUpTheDeputy("")
}

func (c *seatUpContext) mwDeputyIsRunFor(reason string) error {
	return c.bringUpTheDeputy(reason)
}

// bringUpTheDeputy runs the use case as `mw deputy` does: the real vault, the
// real Claude Code harness and the model the real config reads, with only the
// terminal faked.
func (c *seatUpContext) bringUpTheDeputy(reason string) error {
	if err := c.isolateConfig(); err != nil {
		return err
	}
	model, err := config.DeputyModel()
	if err != nil {
		return err
	}
	c.report, c.err = application.Deputy{
		Seats:    c.seatFiles(),
		Windows:  c.windows,
		Harness:  claude.New(),
		Terminal: c.windows,
		Armer:    c.armer,
		Mail:     c.deputyMail(),
		Host:     seatUpHost,
		Reason:   reason,
		Model:    domain.Model(model),
		Now:      func() time.Time { return c.today },
		Out:      &c.said,
	}.Run(context.Background())
	return nil
}

func (c *seatUpContext) deputyRefusedForBeingUp(window string) error {
	said, err := c.refused("the Deputy being up already")
	if err != nil {
		return err
	}
	if !strings.Contains(said, window) || !strings.Contains(said, "already up") {
		return fmt.Errorf("expected the refusal to say the Deputy is already up in %s, got %q", window, said)
	}
	return nil
}

// deputyMail is the Deputy's mail, in memory: one box for the scenario, made
// when it is first asked for.
func (c *seatUpContext) deputyMail() *apptest.FakeMailbox {
	if c.mail == nil {
		c.mail = apptest.NewFakeMailbox()
	}
	return c.mail
}

func (c *seatUpContext) theDeputysBoxHolds(n int) error {
	for i := 0; i < n; i++ {
		if _, err := c.deputyMail().Send(context.Background(), application.NewMessage{
			From: "mayor", To: application.DeputyMailbox, Subject: fmt.Sprintf("task %d", i+1),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *seatUpContext) thePaneIsBusy(window string) error {
	id, open := c.windows.IDOf(window)
	if !open {
		return fmt.Errorf("the window %s is not open", window)
	}
	return c.windows.Pane(id, application.PaneWorking)
}

func (c *seatUpContext) theWindowLosesTheFirstEnter(window string) error {
	return c.loseEnters(window, 1)
}

func (c *seatUpContext) theWindowLosesEveryEnter(window string) error {
	return c.loseEnters(window, -1)
}

func (c *seatUpContext) loseEnters(window string, n int) error {
	id, open := c.windows.IDOf(window)
	if !open {
		return fmt.Errorf("the window %s is not open", window)
	}
	c.windows.LoseEnters(id, n)
	return nil
}

func (c *seatUpContext) enterWasPressed(times int, window string) error {
	id, open := c.windows.IDOf(window)
	if !open {
		return fmt.Errorf("the window %s is not open", window)
	}
	if got := c.windows.Enters(id); got != times {
		return fmt.Errorf("expected Enter pressed %d times in %s, it was pressed %d", times, window, got)
	}
	return nil
}

func (c *seatUpContext) deputyRefusedForBeingBusy(window string) error {
	said, err := c.refused("the Deputy being busy")
	if err != nil {
		return err
	}
	want := "the Deputy is busy in window " + window + "; the mail waits"
	if !strings.Contains(said, want) {
		return fmt.Errorf("expected the refusal to say %q, got %q", want, said)
	}
	return nil
}

func (c *seatUpContext) deputySaysItNudged(window string) error {
	if c.err != nil {
		return fmt.Errorf("expected mw deputy to succeed, got: %w", c.err)
	}
	want := "nudged the Deputy in window " + window
	if !strings.Contains(c.said.String(), want) {
		return fmt.Errorf("expected mw deputy to say %q, it said %q", want, c.said.String())
	}
	return nil
}

func (c *seatUpContext) deputySaysEnterAgain() error {
	if !strings.Contains(c.said.String(), "pressed Enter again") || !strings.Contains(c.said.String(), "and it went") {
		return fmt.Errorf("expected mw deputy to say it pressed Enter again and it went, it said %q", c.said.String())
	}
	return nil
}

func (c *seatUpContext) deputySaysStuck() error {
	if !strings.Contains(c.said.String(), "is on its input line still") {
		return fmt.Errorf("expected mw deputy to say the nudge is on its input line still, it said %q", c.said.String())
	}
	return nil
}

func (c *seatUpContext) typedInto(window string) ([]string, error) {
	id, open := c.windows.IDOf(window)
	if !open {
		return nil, fmt.Errorf("the window %s is not open", window)
	}
	return c.windows.Typed(id), nil
}

func (c *seatUpContext) theLineWasTyped(line, window string) error {
	typed, err := c.typedInto(window)
	if err != nil {
		return err
	}
	if len(typed) != 1 || typed[0] != line+"\n" {
		return fmt.Errorf("expected exactly the line %q typed into %s, got %q", line, window, typed)
	}
	return nil
}

func (c *seatUpContext) nothingWasTyped(window string) error {
	typed, err := c.typedInto(window)
	if err != nil {
		return err
	}
	if len(typed) != 0 {
		return fmt.Errorf("expected nothing typed into %s, got %q", window, typed)
	}
	return nil
}

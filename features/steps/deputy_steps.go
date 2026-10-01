package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
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
	ctx.When(`^mw deputy is run$`, c.mwDeputyIsRun)
	ctx.When(`^mw deputy is run for the reason "([^"]*)"$`, c.mwDeputyIsRunFor)

	ctx.Then(`^mw deputy succeeds$`, c.seatUpSucceeds)
	ctx.Then(`^mw deputy is refused saying the Deputy is already up in "([^"]*)"$`, c.deputyRefusedForBeingUp)
	ctx.Then(`^mw deputy is refused saying there is no charter$`, c.refusedForNoCharter)
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

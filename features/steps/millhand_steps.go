package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/cucumber/godog"
)

// The steps of features/millhand.feature. mw millhand is a thin command over
// mw seat up, so its scenarios are the seat up context's: the same vault on
// disk, the same faked terminal and the same window assertions. What they add is
// the wake, the review mark, and a config file the models are read from.

// isolateConfig points the process at a home directory of its own and clears
// the environment the Millhand's models are read from, so that no scenario ever
// reads this machine's own config file. restoreConfig undoes it. It is done by
// the steps that read the config and by them alone — every feature's hooks run
// for every scenario, and another feature's own home directory is not to be
// replaced — and once for a scenario, whichever of them comes first.
func (c *seatUpContext) isolateConfig() error {
	if c.home != "" {
		return nil
	}
	home, err := os.MkdirTemp("", "mw-millhand-home-")
	if err != nil {
		return fmt.Errorf("making a home directory: %w", err)
	}
	c.home, c.homeWas = home, os.Getenv("HOME")
	c.modelEnvWas = map[string]*string{}
	for _, env := range []string{config.MillhandRoutineModelEnv, config.MillhandReviewModelEnv} {
		if was, set := os.LookupEnv(env); set {
			c.modelEnvWas[env] = &was
		} else {
			c.modelEnvWas[env] = nil
		}
		if err := os.Unsetenv(env); err != nil {
			return err
		}
	}
	return os.Setenv("HOME", home)
}

func (c *seatUpContext) restoreConfig() {
	if c.home == "" {
		return
	}
	_ = os.Setenv("HOME", c.homeWas)
	for env, was := range c.modelEnvWas {
		if was == nil {
			_ = os.Unsetenv(env)
		} else {
			_ = os.Setenv(env, *was)
		}
	}
	_ = os.RemoveAll(c.home)
}

// registerMillhandSteps adds the steps of features/millhand.feature to the seat
// up scenario.
func registerMillhandSteps(ctx *godog.ScenarioContext, c *seatUpContext) {
	ctx.Given(`^the "([^"]*)" seat's review mark says "([^"]*)"$`, c.theReviewMarkSays)
	ctx.Given(`^the config file sets "([^"]*)" to "([^"]*)"$`, c.theConfigFileSets)

	ctx.When(`^mw millhand is run with no wake named$`, c.mwMillhandIsRunWithNoWake)
	ctx.When(`^mw millhand is run for a "([^"]*)" wake$`, c.mwMillhandIsRunForAWake)
	ctx.When(`^mw millhand is run for a "([^"]*)" wake for the reason "([^"]*)"$`, c.mwMillhandIsRunForAWakeFor)

	ctx.Then(`^mw millhand succeeds$`, c.seatUpSucceeds)
	ctx.Then(`^mw millhand fails$`, c.millhandFails)
	ctx.Then(`^mw millhand is refused saying the Millhand is already up in "([^"]*)"$`, c.millhandRefusedForBeingUp)
	ctx.Then(`^mw millhand is refused saying "([^"]*)" is not a kind of wake$`, c.millhandRefusedForTheWake)
	ctx.Then(`^mw millhand leaves with the status (\d+)$`, c.millhandLeavesWith)
}

// theReviewMarkSays writes the mark the way the Millhand does: a file beside
// its handoffs on this host, holding when the last review reached.
func (c *seatUpContext) theReviewMarkSays(seat, mark string) error {
	return c.writeSeatFile(mark+"\n", "seats", seat, "hosts", seatUpHost, application.ReviewMarkFileName)
}

// theConfigFileSets adds one root-table key to the config file of this
// scenario's home directory.
func (c *seatUpContext) theConfigFileSets(key, value string) error {
	if err := c.isolateConfig(); err != nil {
		return err
	}
	dir := filepath.Join(c.home, filepath.Dir(config.File))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	file, err := os.OpenFile(filepath.Join(c.home, config.File), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%s = %q\n", key, value)
	return err
}

func (c *seatUpContext) mwMillhandIsRunWithNoWake() error {
	return c.wakeTheMillhand("", "")
}

func (c *seatUpContext) mwMillhandIsRunForAWake(wake string) error {
	return c.wakeTheMillhand(wake, "")
}

func (c *seatUpContext) mwMillhandIsRunForAWakeFor(wake, reason string) error {
	return c.wakeTheMillhand(wake, reason)
}

// wakeTheMillhand runs the use case as `mw millhand` does: the real vault, the
// real Claude Code harness and the models the real config reads, with only the
// terminal faked.
func (c *seatUpContext) wakeTheMillhand(wake, reason string) error {
	if err := c.isolateConfig(); err != nil {
		return err
	}
	routine, err := config.MillhandRoutineModel()
	if err != nil {
		return err
	}
	review, err := config.MillhandReviewModel()
	if err != nil {
		return err
	}
	c.report, c.err = application.Millhand{
		Seats:        c.seatFiles(),
		Windows:      c.windows,
		Harness:      claude.New(),
		Terminal:     c.windows,
		Armer:        c.armer,
		Host:         seatUpHost,
		Wake:         application.Wake(wake),
		Reason:       reason,
		RoutineModel: domain.Model(routine),
		ReviewModel:  domain.Model(review),
		Now:          func() time.Time { return c.today },
		Out:          &c.said,
	}.Run(context.Background())
	return nil
}

func (c *seatUpContext) millhandFails() error {
	if c.err == nil {
		return fmt.Errorf("expected mw millhand to fail, it said %q", c.report.String())
	}
	return nil
}

func (c *seatUpContext) millhandRefusedForBeingUp(window string) error {
	said, err := c.refused("the Millhand being up already")
	if err != nil {
		return err
	}
	if !strings.Contains(said, window) || !strings.Contains(said, "already up") {
		return fmt.Errorf("expected the refusal to say the Millhand is already up in %s, got %q", window, said)
	}
	return nil
}

func (c *seatUpContext) millhandRefusedForTheWake(wake string) error {
	said, err := c.refused("an unknown wake")
	if err != nil {
		return err
	}
	if !strings.Contains(said, wake) {
		return fmt.Errorf("expected the refusal to name the wake %q, got %q", wake, said)
	}
	return nil
}

func (c *seatUpContext) millhandLeavesWith(status int) error {
	if got := application.ExitStatus(c.err); got != status {
		return fmt.Errorf("expected mw millhand to leave with %d, it leaves with %d (%v)", status, got, c.err)
	}
	return nil
}

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// bdRetryContext is features/bd_retry.feature's world: a bd with a few beads
// behind it, as a Builder session runs it through mw bd.
type bdRetryContext struct {
	bd     *apptest.FakeBd
	waited time.Duration
	out    strings.Builder
	errs   strings.Builder
	exit   int
}

// InitializeBdRetryScenario registers the steps of features/bd_retry.feature.
func InitializeBdRetryScenario(ctx *godog.ScenarioContext) {
	c := &bdRetryContext{}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = bdRetryContext{}
		return ctx, nil
	})

	ctx.Given(`^a bd with the open steps "([^"]*)" and "([^"]*)"$`, c.aBdWithSteps)
	ctx.Given(`^the next (\d+) writes? to the beads server fail, saying: (.+)$`, c.writesFail)
	ctx.Given(`^the next write to the beads server lands but is reported: (.+)$`, c.aWriteLandsButIsReported)
	ctx.Given(`^the beads server is down, saying: (.+)$`, c.theServerIsDown)

	ctx.When(`^the session runs bd close "([^"]*)"$`, c.theSessionClosesAStep)
	ctx.When(`^the session runs bd comment "([^"]*)" "([^"]*)"$`, c.theSessionComments)

	ctx.Then(`^bd left with status 0$`, func() error { return c.exited(true) })
	ctx.Then(`^bd left with a status that is not 0$`, func() error { return c.exited(false) })
	ctx.Then(`^the step "([^"]*)" is (open|closed)$`, c.theStepIs)
	ctx.Then(`^bd's standard error says "([^"]*)"$`, c.standardErrorSays)
	ctx.Then(`^bd ran the close of "([^"]*)" once$`, c.bdClosedOnce)
	ctx.Then(`^bd waited about (\d+) minutes$`, c.bdWaited)
	ctx.Then(`^the bead "([^"]*)" holds exactly (\d+) comments?, "([^"]*)"$`, c.theBeadHolds)
	ctx.Then(`^bd was run (\d+) times?$`, c.bdWasRun)
}

func (c *bdRetryContext) aBdWithSteps(a, b string) error {
	c.bd = apptest.NewFakeBd(a, b)
	return nil
}

func (c *bdRetryContext) writesFail(n int, said string) error {
	faults := make([]apptest.BdFault, n)
	for i := range faults {
		faults[i] = apptest.BdFault{Said: strings.TrimSpace(said)}
	}
	c.bd.FailWrites(faults...)
	return nil
}

func (c *bdRetryContext) aWriteLandsButIsReported(said string) error {
	c.bd.FailWrites(apptest.BdFault{Said: strings.TrimSpace(said), Landed: true})
	return nil
}

func (c *bdRetryContext) theServerIsDown(said string) error {
	c.bd.GoDown(strings.TrimSpace(said))
	return nil
}

func (c *bdRetryContext) run(args ...string) error {
	exit, err := application.BdRetry{
		Program: c.bd,
		Wait: func(_ context.Context, d time.Duration) error {
			c.waited += d
			return nil
		},
		Out: &c.out, Err: &c.errs,
	}.Run(context.Background(), args)
	c.exit = exit
	return err
}

func (c *bdRetryContext) theSessionClosesAStep(id string) error {
	return c.run("-C", "/vault", "close", id, "--reason", "done")
}

func (c *bdRetryContext) theSessionComments(id, text string) error {
	return c.run("-C", "/vault", "comment", id, text)
}

func (c *bdRetryContext) exited(zero bool) error {
	if (c.exit == 0) != zero {
		return fmt.Errorf("bd left with status %d, standard error:\n%s", c.exit, c.errs.String())
	}
	return nil
}

func (c *bdRetryContext) theStepIs(id, want string) error {
	if got := c.bd.Status(id); got != want {
		return fmt.Errorf("%s is %s, want %s", id, got, want)
	}
	return nil
}

func (c *bdRetryContext) standardErrorSays(words string) error {
	if !strings.Contains(c.errs.String(), words) {
		return fmt.Errorf("expected standard error to say %q, it said:\n%s", words, c.errs.String())
	}
	return nil
}

func (c *bdRetryContext) bdClosedOnce(id string) error {
	closes := 0
	for _, call := range c.bd.Calls() {
		for i, arg := range call {
			if arg == "close" && i+1 < len(call) && call[i+1] == id {
				closes++
			}
		}
	}
	if closes != 1 {
		return fmt.Errorf("bd ran the close of %s %d times, want once: %v", id, closes, c.bd.Calls())
	}
	return nil
}

func (c *bdRetryContext) bdWaited(minutes int) error {
	want := time.Duration(minutes) * time.Minute
	if c.waited < want-30*time.Second || c.waited > want+30*time.Second {
		return fmt.Errorf("bd waited %s, want about %s", c.waited, want)
	}
	return nil
}

func (c *bdRetryContext) theBeadHolds(id string, n int, text string) error {
	got := c.bd.Comments(id)
	if len(got) != n || (n == 1 && got[0] != text) {
		return fmt.Errorf("%s holds %d comments %q, want %d (%q)", id, len(got), got, n, text)
	}
	return nil
}

func (c *bdRetryContext) bdWasRun(n int) error {
	if got := len(c.bd.Calls()); got != n {
		return fmt.Errorf("bd was run %d times, want %d: %v", got, n, c.bd.Calls())
	}
	return nil
}

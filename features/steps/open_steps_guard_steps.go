package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// The steps of features/open_steps_guard.feature run the Stop hook's guard of a
// Builder's session in next.feature's world, then the close-out and the report
// of what it refused (mw-gq6.1676).

// registerOpenStepsGuardSteps registers them with nextContext.
func registerOpenStepsGuardSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.When(`^the session of "([^"]*)" tries to end and the guard is asked$`, c.theGuardIsAsked)
	ctx.Then(`^the guard keeps the session going, naming the open step$`, c.theGuardKeepsTheSessionGoing)
	ctx.Then(`^the guard lets the session end$`, c.theGuardLetsTheSessionEnd)
	ctx.When(`^the Builder of "([^"]*)" marks the open step the Governor's, saying "([^"]*)"$`, c.theBuilderMarksTheOpenStep)
	ctx.Then(`^the report counts the open-steps refusal as hands waiting on him, and none as left open$`, c.theReportCountsAHandsWait)
}

// guardAnswer is what the last ask of the guard came to.
type guardAnswer struct {
	blocked bool
	said    string
	err     error
}

var lastGuardAnswer = map[*nextContext]guardAnswer{}

func (c *nextContext) openStepOf(id string) (application.FormulaStep, error) {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return application.FormulaStep{}, err
	}
	open, err := c.tracker.OpenSteps(context.Background(), detail.Molecule.RootID)
	if err != nil {
		return application.FormulaStep{}, err
	}
	if len(open) != 1 {
		return application.FormulaStep{}, fmt.Errorf("expected one open step of %s, got %d", id, len(open))
	}
	return open[0], nil
}

func (c *nextContext) theGuardIsAsked(id string) error {
	var said strings.Builder
	blocked, err := application.OpenStepsGuard{
		Tracker: c.tracker, Notes: c.tracker, Records: vault.New(c.vault), Out: &said, Vault: c.vault,
	}.Run(context.Background(), id, strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop","stop_hook_active":false}`))
	lastGuardAnswer[c] = guardAnswer{blocked: blocked, said: said.String(), err: err}
	return nil
}

func (c *nextContext) theGuardKeepsTheSessionGoing() error {
	answer := lastGuardAnswer[c]
	if answer.err != nil || !answer.blocked {
		return fmt.Errorf("expected the guard to keep the session going, got blocked %v, err %v", answer.blocked, answer.err)
	}
	step, err := c.openStepOf("mw-gq6.1")
	if err != nil {
		return err
	}
	if !strings.Contains(answer.said, step.ID+" · "+step.Title) {
		return fmt.Errorf("expected the guard to name %s · %s, said:\n%s", step.ID, step.Title, answer.said)
	}
	return nil
}

func (c *nextContext) theGuardLetsTheSessionEnd() error {
	answer := lastGuardAnswer[c]
	if answer.err != nil || answer.blocked {
		return fmt.Errorf("expected the guard to let the session end, got blocked %v, err %v, said:\n%s", answer.blocked, answer.err, answer.said)
	}
	return nil
}

func (c *nextContext) theBuilderMarksTheOpenStep(id, reason string) error {
	step, err := c.openStepOf(id)
	if err != nil {
		return err
	}
	_, err = application.GovernorsStepMark{
		Tracker: c.tracker, Notes: c.tracker,
		Now: func() time.Time { return time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC) },
	}.Run(context.Background(), id, step.ID, reason)
	return err
}

func (c *nextContext) theReportCountsAHandsWait() error {
	var out strings.Builder
	_, err := application.FactoryReport{
		Tracker: c.tracker, Files: vault.New(c.vault), Seat: nextSeat, Benchmarks: &apptest.FakeBenchmarks{}, Hands: c.tracker,
		Now: func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }, Out: &out,
	}.Run(context.Background())
	if err != nil {
		return err
	}
	if want := "open-steps 1: left open 0 · hands waiting on him 1"; !strings.Contains(out.String(), want) {
		return fmt.Errorf("expected the report to say %q, got:\n%s", want, out.String())
	}
	return nil
}

package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of the scenarios of features/next_gate_lock.feature in which the
// target branch moves while a close-out waits for this host's gate lock
// (mw-gq6.872): the lock is found held by another gate on the ask the scenario
// names, and by the time the close-out has it, that gate has landed the fix of
// the rig's red on the target at the origin.

const movingTargetFix = "fix.md"

func registerNextMovingTargetSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's tests fail, saying "([^"]*)", with the other host's work merged in unless the target holds the fix$`, c.theRigsTestsFailUnlessTheTargetHoldsTheFix)
	ctx.Given(`^the rig has a named-tests command that fails on main alone unless the target holds the fix, saying "([^"]*)"$`, c.theNamedTestsFailUnlessTheTargetHoldsTheFix)
	ctx.Given(`^"([^"]*)" at the origin gets the fix while the gate lock is asked for the (\d+)(?:st|nd|rd|th) time and found held$`, c.theTargetGetsTheFixWhileTheLockIsWaited)
	ctx.Then(`^"([^"]*)" at the origin holds the fix$`, c.theOriginHoldsTheFix)
}

func (c *nextContext) theRigsTestsFailUnlessTheTargetHoldsTheFix(saying string) error {
	c.checkCommand = fmt.Sprintf(
		"if [ -f %s ] && [ ! -f %s ]; then printf 'run\\n' >> %s; echo '%s'; exit 1; fi; printf 'run\\n' >> %s",
		nextOtherFile, movingTargetFix, c.checkLog, saying, c.checkLog)
	return nil
}

func (c *nextContext) theNamedTestsFailUnlessTheTargetHoldsTheFix(saying string) error {
	c.setNamedCommand(fmt.Sprintf("set -- {tests}; printf 'run %%s\\n' \"$*\" >> %s; [ -f %s ] && exit 0; echo '%s'; exit 1", c.namedLog(), movingTargetFix, saying))
	return nil
}

func (c *nextContext) theTargetGetsTheFixWhileTheLockIsWaited(_ string, ask int) error {
	c.moveAt = ask
	return nil
}

// landTheFix is the gate that held the lock ending after it landed the fix.
func (c *nextContext) landTheFix() error {
	if err := os.WriteFile(filepath.Join(c.seed, movingTargetFix), []byte("the fix of the red\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "Fix the red"}, {"push", "-q", "origin", "main"},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theOriginHoldsTheFix(branch string) error {
	if _, err := gitSay(c.origin(), "show", branch+":"+movingTargetFix); err != nil {
		return fmt.Errorf("expected %s at the origin to hold %s: %w", branch, movingTargetFix, err)
	}
	return nil
}

// movingGate is the gate lock of a scenario in which the lock is found held on
// one ask, and the target has the fix by the time the ask is answered.
type movingGate struct {
	inner application.GateLock
	c     *nextContext
}

func (g movingGate) Take(ctx context.Context, holder string, held func(by string)) (application.GateTurn, error) {
	g.c.takes++
	if g.c.takes == g.c.moveAt {
		if held != nil {
			held("mw-gq6.9")
		}
		if err := g.c.landTheFix(); err != nil {
			return application.GateTurn{}, err
		}
		if err := ctx.Err(); err != nil {
			return application.GateTurn{}, err
		}
	}
	return g.inner.Take(ctx, holder, held)
}

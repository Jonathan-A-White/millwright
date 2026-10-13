package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"

	"github.com/cucumber/godog"
)

// The steps of features/next_gate_lock.feature run the close-out of
// next.feature's world with this host's gate lock: a real flock in the
// scenario's own directory, and another close-out's gate that is a stand-in
// holding the same lock.

// registerNextGateSteps registers them with nextContext.
func registerNextGateSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^this host's gate lock is held by the gate of "([^"]*)", which ends once another gate is seen waiting$`, c.theGateLockIsHeldByAnotherGate)

	ctx.Then(`^the rig's tests of "([^"]*)" began after the gate of "([^"]*)" ended$`, c.theTestsBeganAfterTheOtherGateEnded)
	ctx.Then(`^the printed report says the gate waited for "([^"]*)"$`, c.thePrintedReportSaysTheGateWaited)
	ctx.Then(`^the mail to the Mayor says the gate waited for "([^"]*)"$`, c.theMailSaysTheGateWaited)
	ctx.Then(`^the printed report says nothing of waiting for the gate lock$`, c.thePrintedReportSaysNothingOfTheGateLock)
	ctx.Then(`^this host's gate lock is free again$`, c.theGateLockIsFree)
}

// gateWaitLine is how the report says a gate waited, with the wait as the
// report writes it: seconds, minutes and seconds, or under a second.
func gateWaitLine(story string) *regexp.Regexp {
	return regexp.MustCompile(`gate waited (under a second|\d+[hms]\S*) for ` + regexp.QuoteMeta(story))
}

// gateDir is where this scenario's host keeps its gate lock.
func (c *nextContext) gateDir() string { return filepath.Join(c.root, "state") }

func (c *nextContext) quickGate() *hostlock.Gate {
	return hostlock.NewGate(c.gateDir(), hostlock.WithGatePoll(20*time.Millisecond), hostlock.WithGateCap(time.Minute))
}

// gateLock is the gate lock mw next is given. When another gate holds it, the
// other gate ends the moment this one is seen waiting, which is how two gates
// that would overlap are made to overlap on purpose.
func (c *nextContext) gateLock() application.GateLock {
	if c.otherGate == nil {
		return c.quickGate()
	}
	return seenGate{inner: c.quickGate(), onHeld: c.otherGateEnds}
}

// seenGate tells a scenario the instant a gate is found waiting.
type seenGate struct {
	inner  application.GateLock
	onHeld func()
}

func (g seenGate) Take(ctx context.Context, holder string, held func(by string)) (application.GateTurn, error) {
	return g.inner.Take(ctx, holder, func(by string) {
		g.onHeld()
		if held != nil {
			held(by)
		}
	})
}

func (c *nextContext) theGateLockIsHeldByAnotherGate(story string) error {
	turn, err := c.quickGate().Take(context.Background(), story, nil)
	if err != nil {
		return err
	}
	c.otherGate, c.otherGateStory = turn.Holding, story
	return nil
}

// otherGateEnds is the other gate finishing: it writes that down where the
// rig's tests write their runs, and lets go of the lock.
func (c *nextContext) otherGateEnds() {
	log, err := os.OpenFile(c.checkLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintf(log, "gate of %s ended\n", c.otherGateStory)
		log.Close()
	}
	c.otherGate.Release(context.Background())
}

func (c *nextContext) theTestsBeganAfterTheOtherGateEnded(story, other string) error {
	ran, err := os.ReadFile(c.checkLog)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(string(ran))
	ended := fmt.Sprintf("gate of %s ended", other)
	if !strings.HasPrefix(text, ended) || !strings.Contains(text, "run") {
		return fmt.Errorf("expected the gate of %s to end before the tests of %s ran, the tests' log says:\n%s", other, story, text)
	}
	return nil
}

func (c *nextContext) thePrintedReportSaysTheGateWaited(story string) error {
	if !gateWaitLine(story).MatchString(c.printed.String()) {
		return fmt.Errorf("expected the report to say the gate waited for %s, it printed:\n%s", story, c.printed.String())
	}
	return nil
}

func (c *nextContext) theMailSaysTheGateWaited(story string) error {
	sent, err := c.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		return err
	}
	for _, mail := range sent {
		if gateWaitLine(story).MatchString(mail.Body) {
			return nil
		}
	}
	return fmt.Errorf("expected the mail to the Mayor to say the gate waited for %s, got %+v", story, sent)
}

func (c *nextContext) thePrintedReportSaysNothingOfTheGateLock() error {
	if strings.Contains(c.printed.String(), "gate waited") {
		return fmt.Errorf("expected no word of waiting for the gate lock, the report printed:\n%s", c.printed.String())
	}
	return nil
}

// theGateLockIsFree says the lock can be taken at once, which is the only
// honest way to ask: an advisory lock is held by the kernel, not by a file.
func (c *nextContext) theGateLockIsFree() error {
	turn, err := hostlock.NewGate(c.gateDir(), hostlock.WithGateCap(time.Second), hostlock.WithGatePoll(20*time.Millisecond)).
		Take(context.Background(), "the scenario", nil)
	if err != nil {
		return fmt.Errorf("expected this host's gate lock to be free: %w", err)
	}
	return turn.Holding.Release(context.Background())
}

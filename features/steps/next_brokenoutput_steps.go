package steps

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// brokenOutput is the output of a close-out whose pipe lost its reader: every
// write fails, as a write to a closed pipe does once SIGPIPE is not fatal.
type brokenOutput struct{}

func (brokenOutput) Write([]byte) (int, error) { return 0, errors.New("write: broken pipe") }

// dispatchOut is where the dispatch the close-out chains prints: nowhere, unless
// the scenario broke the close-out's output (mw-gq6.1580).
func (c *nextContext) dispatchOut() io.Writer {
	if c.outputBroken {
		return brokenOutput{}
	}
	return nil
}

func (c *nextContext) registerBrokenOutputSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the close-out's output has lost its reader$`, func() error { c.outputBroken = true; return nil })
	ctx.Then(`^no story of this host is left claimed with no session$`, c.noClaimWithoutASession)
}

// noClaimWithoutASession reads the claimed set against the sessions started: a
// story in progress here whose session the runner does not hold is a claim that
// never launched.
func (c *nextContext) noClaimWithoutASession() error {
	claimed, err := c.tracker.RunningStories(context.Background(), nextHost)
	if err != nil {
		return err
	}
	sessions := map[string]bool{}
	for _, name := range c.runner.Names() {
		sessions[name] = true
	}
	for _, detail := range claimed {
		if id := detail.Story.ID; !sessions[application.SessionName(id)] && id != "mw-gq6.1" {
			return fmt.Errorf("%s is claimed here and no session was started for it (sessions: %q)", id, c.runner.Names())
		}
	}
	return nil
}

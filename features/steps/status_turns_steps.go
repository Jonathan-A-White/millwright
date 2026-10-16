package steps

import (
	"context"
	"path/filepath"

	"github.com/cucumber/godog"
)

// registerStatusTurnsSteps registers the steps of features/status.feature about
// the turns a running session has taken (mw-1ljpo9.6).
func registerStatusTurnsSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^the session of "([^"]*)" has taken (\d+) turns so far$`, c.theSessionHasTakenTurnsSoFar)
}

func (c *statusContext) theSessionHasTakenTurnsSoFar(id string, turns int) error {
	if c.turns == nil {
		c.turns = statusTurns{}
	}
	c.turns[id] = turns
	return nil
}

// statusTurns is the turns each story's session has taken, by story id: the
// worktree a session runs in is named after its story.
type statusTurns map[string]int

func (s statusTurns) Turns(_ context.Context, dir string) (int, error) {
	return s[filepath.Base(dir)], nil
}

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// The steps of features/next_files.feature (mw-gq6.406): the files a running
// story's branch has changed are recorded on the story by the heartbeat, and a
// landing says which of them its files key did not name.

// registerNextFilesSteps registers them with nextContext.
func registerNextFilesSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the terminal holds a running session for "([^"]*)"$`, c.theTerminalHoldsARunningSession)
	ctx.Given(`^the story "([^"]*)" expects to change "([^"]*)"$`, c.theStoryExpectsToChange)
	ctx.When(`^the heartbeat of "([^"]*)" ticks once$`, c.theHeartbeatTicksOnce)
	ctx.Then(`^the story "([^"]*)" records "([^"]*)" as changed$`, c.theStoryRecordsAsChanged)
	ctx.Then(`^the story "([^"]*)" records no files as changed$`, c.theStoryRecordsNoFilesAsChanged)
}

func (c *nextContext) theTerminalHoldsARunningSession(id string) error {
	return c.runner.Start(context.Background(), application.SessionSpec{Name: application.SessionName(id), Command: []string{"claude"}})
}

func (c *nextContext) theStoryExpectsToChange(id, files string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{"files": files})
}

// theHeartbeatTicksOnce runs the heartbeat through one wait: the first wait
// passes, and the second ends the loop.
func (c *nextContext) theHeartbeatTicksOnce(id string) error {
	waits := 0
	return application.Next{
		Tracker: c.tracker,
		Runner:  c.runner,
		Changes: rig.New(),
		Rigs:    map[string]string{c.rigKey(): c.rig},
		Out:     &c.printed,
		HeartbeatWait: func(context.Context, time.Duration) error {
			if waits++; waits > 1 {
				return context.Canceled
			}
			return nil
		},
	}.Heartbeat(context.Background(), id)
}

func (c *nextContext) theStoryRecordsAsChanged(id, files string) error {
	if got := c.tracker.Metadata(id)[application.FilesChangedField]; got != files {
		return fmt.Errorf("expected %s to record %q as changed, got %q", id, files, got)
	}
	return nil
}

func (c *nextContext) theStoryRecordsNoFilesAsChanged(id string) error {
	if got := strings.TrimSpace(c.tracker.Metadata(id)[application.FilesChangedField]); got != "" {
		return fmt.Errorf("expected %s to record no files as changed, got %q", id, got)
	}
	return nil
}

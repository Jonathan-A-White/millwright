package steps

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// registerBoostAsleepSteps registers the steps of the boost-asleep scenarios.
func (c *doctorContext) registerBoostAsleepSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the home host's boost-asleep check, the desktop having last synced (\d+) hours? ago$`, c.theHomeHostsBoostAsleepCheck)
	ctx.When(`^mw doctor's boost-asleep check runs$`, func() error { return c.run(false) })
}

func (c *doctorContext) theHomeHostsBoostAsleepCheck(hoursText string) error {
	hours, err := strconv.Atoi(hoursText)
	if err != nil {
		return fmt.Errorf("parsing %q as hours: %w", hoursText, err)
	}
	said := c.now.Add(-time.Duration(hours) * time.Hour).UTC().Format(application.LastSyncFormat)
	if err := c.notes.SetNote(context.Background(), application.LastSyncKey("desktop"), said); err != nil {
		return err
	}
	check := doctor.NewBoostAsleep(&apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}, "laptop", c.notes)
	check.Limit = application.DefaultHostSilence
	check.Now = func() time.Time { return c.now }
	c.real = check
	return nil
}

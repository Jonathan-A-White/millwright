package steps

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"context"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// givesWayFrom is the host's give-way marker for a batch landing: the host is free
// at the first looks the batch makes at it, and gives way from the look numbered
// from on, as when the person at its screen turns give way on while the batch is
// made (mw-2891blf.19).
type givesWayFrom struct {
	mu    sync.Mutex
	from  int
	looks int
}

func (g *givesWayFrom) Standing(context.Context, bool) (application.GiveWayStanding, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.looks++
	if g.looks < g.from {
		return application.GiveWayStanding{}, false, nil
	}
	return application.GiveWayStanding{Since: time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC), By: "mw@laptop"}, true, nil
}

// registerBatchLandGiveWaySteps registers the give-way steps of
// features/batch_land.feature (mw-2891blf.19).
func registerBatchLandGiveWaySteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the host gives way from the batch's look number (\d+) at its give-way marker$`, func(from int) error {
		c.batch.giveWay = &givesWayFrom{from: from}
		return nil
	})
	ctx.Then(`^the batch is held because the host gives way, and says since when$`, func() error {
		held := c.batch.report.Held
		if !strings.Contains(held, "gives way since 2026-11-13T16:40:00Z (mw@laptop)") {
			return fmt.Errorf("expected the batch held because the host gives way, got %q\n%s", held, c.said())
		}
		return nil
	})
}

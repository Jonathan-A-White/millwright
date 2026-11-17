package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// stuckHoldNow is the clock of the stuck-hold scenarios.
var stuckHoldNow = time.Date(2026, 11, 16, 7, 30, 0, 0, time.UTC)

// stuckHoldContext is a fake bead graph and tracker holding the same beads, and
// what mw stuck found.
type stuckHoldContext struct {
	graph    *apptest.FakeBeadGraph
	tracker  *apptest.FakeTracker
	comments map[string][]application.Comment
	held     []application.TriageBead
	report   application.StuckReport
}

// stuckHoldBeads is the sleep pass's view of the held beads: application.SleepHolds.
type stuckHoldBeads struct{ c *stuckHoldContext }

func (h stuckHoldBeads) TriageBeads(context.Context) ([]application.TriageBead, error) {
	return h.c.held, nil
}

func (h stuckHoldBeads) StoriesComments(context.Context, []string) (map[string][]application.Comment, error) {
	return h.c.comments, nil
}

// InitializeStuckHoldScenario registers the steps of features/stuck.feature.
func InitializeStuckHoldScenario(ctx *godog.ScenarioContext) {
	c := &stuckHoldContext{}
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = stuckHoldContext{graph: &apptest.FakeBeadGraph{}, tracker: apptest.NewFakeTracker(), comments: map[string][]application.Comment{}}
		c.tracker.AddEpic("mw-s", domain.Path{})
		return ctx, nil
	})
	ctx.Given(`^a held bead "([^"]*)" whose only comment is "([^"]*)"$`, c.aHeldBead)
	ctx.Given(`^a live bead "([^"]*)" that waits behind "([^"]*)"$`, c.aLiveBeadBehind)
	ctx.When(`^mw stuck looks$`, c.stuckLooks)
	ctx.Then(`^mw stuck lists no root$`, c.noRoot)
	ctx.Then(`^mw stuck lists the root "([^"]*)"$`, c.theRoot)
	ctx.Then(`^the sleep pass's old-holds line for "([^"]*)" has the reason "([^"]*)"$`, c.theOldHoldReason)
}

func (c *stuckHoldContext) aHeldBead(id, comment string) error {
	c.graph.Add(id, "Bead "+id, "deferred", "task", nil)
	c.tracker.AddStory("mw-s", domain.Story{ID: id, Title: "Bead " + id})
	if err := c.tracker.CommentOnStory(context.Background(), id, comment); err != nil {
		return err
	}
	then := stuckHoldNow.Add(-10 * 24 * time.Hour)
	c.held = append(c.held, application.TriageBead{ID: id, Title: "Bead " + id, Status: "deferred", Created: then, Updated: then})
	c.comments[id] = []application.Comment{{Created: then, Text: comment}}
	return nil
}

func (c *stuckHoldContext) aLiveBeadBehind(id, behind string) error {
	c.graph.Add(id, "Bead "+id, "open", "task", nil, domain.BeadEdge{To: behind, Kind: "blocks"})
	c.tracker.AddStory("mw-s", domain.Story{ID: id, Title: "Bead " + id})
	return nil
}

func (c *stuckHoldContext) stuckLooks() error {
	var err error
	c.report, err = application.StuckSurvey{
		Graph: c.graph, Comments: c.tracker, Claims: c.tracker, Now: func() time.Time { return stuckHoldNow },
	}.Find(context.Background())
	return err
}

func (c *stuckHoldContext) noRoot() error {
	if len(c.report.Roots) != 0 {
		return fmt.Errorf("mw stuck lists %+v, want no root", c.report.Roots)
	}
	return nil
}

func (c *stuckHoldContext) theRoot(id string) error {
	for _, r := range c.report.Roots {
		if r.ID == id {
			return nil
		}
	}
	return fmt.Errorf("mw stuck lists %+v, want the root %s", c.report.Roots, id)
}

func (c *stuckHoldContext) theOldHoldReason(id, want string) error {
	holds, err := application.OldHolds(context.Background(), stuckHoldBeads{c}, stuckHoldNow)
	if err != nil {
		return err
	}
	for _, h := range holds {
		if h.ID == id {
			if !strings.Contains(h.Reason, want) {
				return fmt.Errorf("the old-holds line for %s has the reason %q, want %q", id, h.Reason, want)
			}
			return nil
		}
	}
	return fmt.Errorf("the old-holds lines %+v do not name %s", holds, id)
}

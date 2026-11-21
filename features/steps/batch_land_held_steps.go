package steps

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// batchHeldContext is one scenario of features/batch_land_held.feature: a
// follower on lampas over a fake tracker, and what it asked mw land to do.
type batchHeldContext struct {
	tracker *apptest.FakeTracker
	rules   *apptest.FakeEpicRules
	history *apptest.FakeBatchMetrics
	now     time.Time
	ids     []string
	batch   int
	red     map[string]bool
	asked   []string
	err     error
}

// InitializeBatchLandHeldScenario registers the steps of features/batch_land_held.feature.
func InitializeBatchLandHeldScenario(ctx *godog.ScenarioContext) {
	c := &batchHeldContext{}
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = batchHeldContext{red: map[string]bool{}}
		return ctx, nil
	})
	ctx.Given(`^lampas lands in batches, with six stories queued, oldest first: lp-1 to lp-6$`, c.sixQueued)
	ctx.Given(`^batch 600 of lampas was held red with all six, its fixer naming "([^"]*)" as the cause$`, c.heldNaming)
	ctx.Given(`^batch 600 of lampas was held red with all six, its fixer naming no story$`, c.heldNamingNone)
	ctx.Given(`^the batch of ([a-z0-9,-]+) is held red again$`, c.redAgain)
	ctx.When(`^the batch-land job runs$`, c.jobRuns)
	ctx.When(`^the Mayor sets (lp-\d) aside, saying "([^"]*)"$`, c.setAside)
	ctx.Then(`^the job asked mw to land lampas, in this order:$`, c.asked_)
	ctx.Then(`^(lp-\d) is held, given back, with a comment opening "([^"]*)" that names its branch "([^"]*)"$`, c.isHeld)
	ctx.Then(`^mw land lampas --set-aside with no why is refused$`, c.refusedWithoutWhy)
}

func (c *batchHeldContext) sixQueued() error {
	c.tracker = apptest.NewFakeTracker()
	c.rules = apptest.NewFakeEpicRules()
	c.history = &apptest.FakeBatchMetrics{}
	c.now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	c.rules.Require("lampas", domain.EpicRequirements{Landing: domain.LandingSettings{Mode: domain.LandingBatch}})
	c.tracker.AddEpic("e-lampas", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Host: "desktop"})
	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("lp-%d", i)
		c.ids = append(c.ids, id)
		c.tracker.AddStory("e-lampas", domain.Story{ID: id, Title: "The story " + id})
		if err := c.tracker.ClaimStory(ctx, id); err != nil {
			return err
		}
		if err := c.tracker.SetStoryState(ctx, id, application.RunState, application.RunQueued, "queued for batch landing"); err != nil {
			return err
		}
		queuedAt := c.now.Add(-time.Duration(300-i*10) * time.Minute).Format(time.RFC3339)
		if err := c.tracker.SetStoryMetadata(ctx, id, map[string]string{application.QueuedAtField: queuedAt}); err != nil {
			return err
		}
	}
	c.batch = 600
	return nil
}

// held leaves the line the batch's report makes, as mw land keeps it.
func (c *batchHeldContext) held(causes ...string) error {
	f := &application.BatchFixing{Started: true}
	for _, id := range causes {
		f.Causes = append(f.Causes, application.FixerWord{Story: id, Said: "its test is wrong"})
	}
	report := application.BatchReport{Rig: "lampas", Number: 600, Tested: true, RedAt: c.now.Add(-time.Hour), Fixer: f}
	for _, id := range c.ids {
		report.Queue = append(report.Queue, application.BatchStory{ID: id})
	}
	return c.history.AppendBatch(context.Background(), report.Metric(c.now.Add(-10*time.Minute)))
}

func (c *batchHeldContext) heldNaming(id string) error { return c.held(id) }
func (c *batchHeldContext) heldNamingNone() error      { return c.held() }

func (c *batchHeldContext) redAgain(ids string) error {
	c.red[ids] = true
	return nil
}

func (c *batchHeldContext) jobRuns() error {
	ctx := context.Background()
	land := func(ctx context.Context, ids []string, label string) error {
		c.asked = append(c.asked, label)
		c.batch++
		if c.red[strings.Join(ids, ",")] {
			if err := c.history.AppendBatch(ctx, application.BatchMetric{Kind: application.BatchKind, At: c.now, Rig: "lampas",
				Batch: c.batch, Stories: len(ids), IDs: ids, Outcome: application.BatchHeld, Red: true}); err != nil {
				return err
			}
			return errors.New("held: the rig's tests on the batch failed")
		}
		work, err := c.tracker.WorkInHand(ctx)
		if err != nil {
			return err
		}
		var landed []string
		for _, d := range work.Running {
			if application.QueuedForBatch(d) && (ids == nil || contains(ids, d.Story.ID)) {
				landed = append(landed, d.Story.ID)
				if err := c.tracker.SetStoryState(ctx, d.Story.ID, application.RunState, application.RunLanded, "landed"); err != nil {
					return err
				}
			}
		}
		return c.history.AppendBatch(ctx, application.BatchMetric{Kind: application.BatchKind, At: c.now, Rig: "lampas",
			Batch: c.batch, Stories: len(landed), IDs: landed, Outcome: application.BatchGreen})
	}
	landings := &application.BatchLandings{
		Rigs: map[string]string{"lampas": "/rigs/lampas"}, Rules: c.rules, Tracker: c.tracker,
		Home:    func(context.Context) (bool, error) { return true, nil },
		History: c.history, Notes: c.tracker, Host: "desktop",
		Now: func() time.Time { return c.now },
		Land: func(ctx context.Context, _ string, _ bool) error {
			return land(ctx, nil, "everything queued")
		},
		LandScoped: func(ctx context.Context, _ string, scope application.LandScope) error {
			return land(ctx, scope.Only, strings.Join(scope.Only, ","))
		},
	}
	c.err = landings.Job().Run(ctx)
	return nil
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func (c *batchHeldContext) asked_(table *godog.Table) error {
	want := tableIDs(table)
	if !reflect.DeepEqual(c.asked, want) {
		return fmt.Errorf("expected mw land asked for %q, in this order, got %q (the pass said: %v)", want, c.asked, c.err)
	}
	return nil
}

func (c *batchHeldContext) setAside(id, why string) error {
	return application.SetAside{Tracker: c.tracker, Host: "desktop"}.Run(context.Background(), "lampas", id, why)
}

func (c *batchHeldContext) isHeld(id, opening, branch string) error {
	ctx := context.Background()
	detail, err := c.tracker.ShowStory(ctx, id)
	if err != nil {
		return err
	}
	if detail.Status != application.StatusHeld || detail.Assignee != "" {
		return fmt.Errorf("expected %s held and given back, got %q held by %q", id, detail.Status, detail.Assignee)
	}
	comments, err := c.tracker.StoryComments(ctx, id)
	if err != nil {
		return err
	}
	if len(comments) == 0 || !strings.HasPrefix(comments[len(comments)-1].Text, opening) || !strings.Contains(comments[len(comments)-1].Text, branch) {
		return fmt.Errorf("expected a comment opening %q and naming %q, got %+v", opening, branch, comments)
	}
	return nil
}

func (c *batchHeldContext) refusedWithoutWhy() error {
	err := application.SetAside{Tracker: c.tracker, Host: "desktop"}.Run(context.Background(), "lampas", "lp-1", "")
	if err == nil || !strings.Contains(err.Error(), "--why") {
		return fmt.Errorf("expected a refusal naming --why, got %v", err)
	}
	return nil
}

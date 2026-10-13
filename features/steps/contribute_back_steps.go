package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"

	"github.com/cucumber/godog"
)

// The steps of features/contribute_back.feature. They run the follower's
// springer over a fake event log and a clock the scenario sets, with the monthly
// contribute-back review as its one job, filing into a fake tracker.

type contributeBackContext struct {
	host, home string
	day        int
	now        time.Time
	tracker    *apptest.FakeTracker
	log        *apptest.FakeEventLog
	spring     *application.EventSpring
	ctx        context.Context
	cancel     context.CancelFunc
}

// InitializeContributeBackScenario registers the steps of features/contribute_back.feature.
func InitializeContributeBackScenario(ctx *godog.ScenarioContext) {
	c := &contributeBackContext{}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = contributeBackContext{
			day:     application.DefaultContributeBackDay,
			tracker: apptest.NewFakeTracker(), log: &apptest.FakeEventLog{},
		}
		c.ctx, c.cancel = context.WithCancel(context.Background())
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		c.cancel()
		if c.spring != nil {
			c.spring.Wait()
		}
		return ctx, nil
	})

	ctx.Given(`^the contribute-back follower is running on "([^"]*)" and the home is "([^"]*)"$`, func(host, home string) error {
		c.host, c.home = host, home
		return nil
	})
	ctx.Given(`^the contribute-back review is due on day (\d+)$`, func(day int) error { c.day = day; return nil })
	ctx.Given(`^the contribute-back clock reads "([^"]*)" UTC$`, c.setClock)
	ctx.When(`^the contribute-back clock reads "([^"]*)" UTC$`, c.setClock)
	ctx.When(`^the contribute-back follower runs a cycle$`, c.cycle)
	ctx.Then(`^(\d+) review beads? titled "([^"]*)" (?:is|are) filed$`, c.filed)
	ctx.Then(`^(\d+) review beads are filed$`, func(n int) error { return c.count("", n) })
	ctx.Then(`^that bead is labelled "([^"]*)" and is under "([^"]*)"$`, c.labelled)
	ctx.Then(`^that bead asks which code we wrote to fill a gap would be welcome upstream$`, c.asks)
	ctx.Then(`^the contribute-back log tells the job "([^"]*)" as ([a-z, ]+)$`, c.logTells)
}

func (c *contributeBackContext) setClock(at string) error {
	t, err := time.Parse("2006-01-02 15:04", at)
	if err != nil {
		return fmt.Errorf("the time %q: %w", at, err)
	}
	c.now = t.UTC()
	return nil
}

func (c *contributeBackContext) cycle() error {
	if c.spring == nil {
		review := &application.ContributeBack{
			Day: c.day, Epic: "mw-gq6", Tracker: c.tracker, Notes: c.tracker,
			Home: func(context.Context) (bool, error) { return c.host == c.home, nil },
			Now:  func() time.Time { return c.now },
		}
		c.spring = &application.EventSpring{Log: c.log, Host: c.host, Jobs: []application.SpringJob{review.Job()}, Now: func() time.Time { return c.now }}
	}
	if err := c.spring.Spring(c.ctx); err != nil {
		return err
	}
	c.spring.Wait()
	return nil
}

// titled is the ids of the beads filed with a title, or all when title is "".
func (c *contributeBackContext) titled(title string) ([]string, error) {
	var ids []string
	for _, id := range c.tracker.Stories() {
		bead, err := c.tracker.ShowStory(c.ctx, id)
		if err != nil {
			return nil, err
		}
		if title == "" || bead.Story.Title == title {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *contributeBackContext) count(title string, want int) error {
	ids, err := c.titled(title)
	if err != nil {
		return err
	}
	if len(ids) != want {
		return fmt.Errorf("expected %d review beads (title %q), got %d: %v", want, title, len(ids), ids)
	}
	return nil
}

func (c *contributeBackContext) filed(n int, title string) error { return c.count(title, n) }

// theBead is the one bead filed, which the "that bead" steps look at.
func (c *contributeBackContext) theBead() (application.StoryDetail, error) {
	ids, err := c.titled("")
	if err != nil {
		return application.StoryDetail{}, err
	}
	if len(ids) != 1 {
		return application.StoryDetail{}, fmt.Errorf("expected one bead filed, got %v", ids)
	}
	return c.tracker.ShowStory(c.ctx, ids[0])
}

func (c *contributeBackContext) labelled(label, epic string) error {
	bead, err := c.theBead()
	if err != nil {
		return err
	}
	has := false
	for _, l := range bead.Labels {
		has = has || l == label
	}
	if !has {
		return fmt.Errorf("the bead carries labels %v, not %q", bead.Labels, label)
	}
	if bead.EpicID != epic {
		return fmt.Errorf("the bead is under %q, not %q", bead.EpicID, epic)
	}
	return nil
}

func (c *contributeBackContext) asks() error {
	bead, err := c.theBead()
	if err != nil {
		return err
	}
	for _, want := range []string{"## Question", "welcome as a pull request", "at most three, only real fits", "decision card"} {
		if !strings.Contains(bead.Description, want) {
			return fmt.Errorf("the bead's description lacks %q:\n%s", want, bead.Description)
		}
	}
	return nil
}

func (c *contributeBackContext) logTells(actor, states string) error {
	var got []string
	for _, e := range c.log.All() {
		if e.Kind == events.KindJob && e.Actor == actor {
			got = append(got, e.To)
		}
	}
	if want := strings.ReplaceAll(states, " ", ""); strings.Join(got, ",") != want {
		return fmt.Errorf("expected %s to be told as %s, got %s", actor, want, strings.Join(got, ","))
	}
	return nil
}

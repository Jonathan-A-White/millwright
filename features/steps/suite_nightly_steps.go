package steps

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"

	"github.com/cucumber/godog"
)

// The steps of features/suite_nightly.feature. They run the follower's springer
// over a fake event log and a clock the scenario sets, with the nightly suite as
// its one job; the suite itself is a function that notes it was asked and keeps
// the day's result in a fake, as mw suite would in the vault.

type suiteNightlyContext struct {
	host, home string
	hour       int
	now        time.Time
	rigs       map[string]string
	rules      *apptest.FakeEpicRules
	records    *apptest.FakeSuiteRecords
	log        *apptest.FakeEventLog
	spring     *application.EventSpring
	ctx        context.Context
	cancel     context.CancelFunc

	mu        sync.Mutex
	ran       []string
	active    int
	together  bool
	cannotRun bool
}

// InitializeSuiteNightlyScenario registers the steps of features/suite_nightly.feature.
func InitializeSuiteNightlyScenario(ctx *godog.ScenarioContext) {
	c := &suiteNightlyContext{}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = suiteNightlyContext{
			hour: application.DefaultNightlySuiteHour, rigs: map[string]string{},
			rules: apptest.NewFakeEpicRules(), records: &apptest.FakeSuiteRecords{}, log: &apptest.FakeEventLog{},
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

	ctx.Given(`^the nightly follower is running on "([^"]*)" and the home is "([^"]*)"$`, func(host, home string) error {
		c.host, c.home = host, home
		return nil
	})
	ctx.Given(`^the rig "([^"]*)" says nightly_suite = true$`, func(rig string) error { return c.rigSays(rig, true) })
	ctx.Given(`^the rig "([^"]*)" says nightly_suite = false$`, func(rig string) error { return c.rigSays(rig, false) })
	ctx.Given(`^the rig "([^"]*)" says nothing of the nightly suite$`, func(rig string) error {
		c.rigs[rig] = "/rigs/" + rig
		return nil
	})
	ctx.Given(`^the nightly suite is sprung at hour (\d+)$`, func(hour int) error { c.hour = hour; return nil })
	ctx.Given(`^the suite of "([^"]*)" cannot be run$`, func(string) error { c.cannotRun = true; return nil })
	ctx.Given(`^the result of "([^"]*)" for "([^"]*)" is already kept$`, c.alreadyKept)
	ctx.Given(`^the nightly clock reads "([^"]*)" UTC$`, c.setClock)
	ctx.When(`^the nightly clock reads "([^"]*)" UTC$`, c.setClock)
	ctx.Given(`^the nightly follower runs a cycle$`, c.cycle)
	ctx.When(`^the nightly follower runs a cycle$`, c.cycle)

	ctx.Then(`^"([^"]*)" was run (\d+) times?$`, c.wasRun)
	ctx.Then(`^no two suites were running together$`, func() error {
		if c.together {
			return fmt.Errorf("two suites were running at once")
		}
		return nil
	})
	ctx.Then(`^the log tells the job "([^"]*)" as ([a-z, ]+)$`, c.logTells)
}

func (c *suiteNightlyContext) rigSays(rig string, on bool) error {
	c.rigs[rig] = "/rigs/" + rig
	c.rules.Require(rig, domain.EpicRequirements{NightlySuite: on})
	return nil
}

func (c *suiteNightlyContext) setClock(at string) error {
	t, err := time.Parse("2006-01-02 15:04", at)
	if err != nil {
		return fmt.Errorf("the time %q: %w", at, err)
	}
	c.now = t.UTC()
	return nil
}

func (c *suiteNightlyContext) alreadyKept(rig, day string) error {
	_, err := c.records.PutSuiteFile(c.ctx, rig, day+".json", `{"rig":"`+rig+`","sha":"abc","passed":true}`)
	return err
}

// suite stands for `mw suite <rig> --file`: it notes it was run, keeps the
// day's result as the real one does, and says if two were running together.
func (c *suiteNightlyContext) suite(ctx context.Context, rig string) error {
	c.mu.Lock()
	c.ran = append(c.ran, "mw suite "+rig+" --file")
	c.active++
	if c.active > 1 {
		c.together = true
	}
	day, cannot := c.now.Format("2006-01-02"), c.cannotRun
	c.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	if cannot {
		return fmt.Errorf("the worktree could not be cut")
	}
	_, err := c.records.PutSuiteFile(ctx, rig, day+".json", `{"rig":"`+rig+`","sha":"abc","passed":true}`)
	return err
}

func (c *suiteNightlyContext) cycle() error {
	if c.spring == nil {
		suite := &application.NightlySuite{
			Hour: c.hour, Rigs: c.rigs, Rules: c.rules, History: c.records,
			Home: func(context.Context) (bool, error) { return c.host == c.home, nil },
			Run:  c.suite,
			Now:  func() time.Time { return c.now },
		}
		c.spring = &application.EventSpring{Log: c.log, Host: c.host, Jobs: []application.SpringJob{suite.Job()}, Now: func() time.Time { return c.now }}
	}
	if err := c.spring.Spring(c.ctx); err != nil {
		return err
	}
	c.spring.Wait()
	return nil
}

func (c *suiteNightlyContext) wasRun(command string, n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	got := 0
	for _, ran := range c.ran {
		if ran == command {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("expected %q run %d times, got %d (all: %v)", command, n, got, c.ran)
	}
	return nil
}

func (c *suiteNightlyContext) logTells(actor, states string) error {
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

package steps

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"

	"github.com/cucumber/godog"
)

// eventClockRestartContext is a follower that is stopped and begun again over
// one event log and one clock, with a clock job of 15 minutes (or dispatch) as
// its job; a pass is counted.
type eventClockRestartContext struct {
	log  *apptest.FakeEventLog
	now  time.Time
	job  string
	jobs []application.SpringJob

	mu   sync.Mutex
	runs map[string]int
}

// InitializeEventClockRestartScenario registers the steps of features/event_clock_restart.feature.
func InitializeEventClockRestartScenario(ctx *godog.ScenarioContext) {
	c := &eventClockRestartContext{}

	ctx.Given(`^a 15-minute clock job on a follower that shares one event log across restarts$`, func() error {
		c.setUp("beads-flush")
		c.jobs = []application.SpringJob{application.ClockJob("beads-flush", 15*time.Minute, c.run("beads-flush"))}
		return nil
	})
	ctx.Given(`^a (\d+)-hour clock job on a follower that shares one event log across restarts$`, func(hours int) error {
		c.setUp("beads-flush")
		c.jobs = []application.SpringJob{application.ClockJob("beads-flush", time.Duration(hours)*time.Hour, c.run("beads-flush"))}
		return nil
	})
	ctx.Given(`^the clock job passed (\d+) hours ago$`, func(n int) error { return c.passed(n * 3600) })
	ctx.Given(`^(\d+) events of no job are written since$`, c.otherEventsSince)
	ctx.Given(`^a follower that shares one event log across restarts and runs dispatch on a heartbeat of an hour$`, func() error {
		c.setUp("dispatch")
		c.jobs = []application.SpringJob{application.DispatchJob(time.Hour, c.run("dispatch"))}
		return nil
	})
	ctx.Given(`^the clock job passed (\d+) minutes ago$`, func(n int) error { return c.passed(n * 60) })
	ctx.Given(`^dispatch passed (\d+) hours ago$`, func(n int) error { return c.passed(n * 3600) })
	ctx.When(`^the follower is restarted (\d+) times, (\d+) minutes apart, springing at each start$`, c.restarts)
	ctx.When(`^(\d+) (?:more )?minutes pass and the follower springs$`, func(n int) error {
		c.now = c.now.Add(time.Duration(n) * time.Minute)
		return c.start()
	})
	ctx.Then(`^the clock job has run at least (\d+) times$`, func(n int) error {
		if got := c.count(); got < n {
			return fmt.Errorf("the clock job ran %d times, want at least %d", got, n)
		}
		return nil
	})
	ctx.Then(`^the clock job has run (\d+) times$`, c.wantRuns)
	ctx.Then(`^dispatch has run (\d+) times$`, c.wantRuns)
}

func (c *eventClockRestartContext) setUp(job string) {
	c.log = &apptest.FakeEventLog{}
	c.now = time.Date(2026, 10, 16, 18, 0, 0, 0, time.UTC)
	c.job = job
	c.runs = map[string]int{}
}

func (c *eventClockRestartContext) run(name string) func(context.Context) error {
	return func(context.Context) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.runs[name]++
		return nil
	}
}

func (c *eventClockRestartContext) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runs[c.job]
}

func (c *eventClockRestartContext) wantRuns(n int) error {
	if got := c.count(); got != n {
		return fmt.Errorf("%s ran %d times, want %d", c.job, got, n)
	}
	return nil
}

// passed puts the end of a pass of the job into the log as it was secs ago.
func (c *eventClockRestartContext) passed(secs int) error {
	e := events.Event{Kind: events.KindJob, Actor: c.job + "@laptop", From: events.JobRunning, To: events.JobDone, Ts: c.now.Add(-time.Duration(secs) * time.Second), Lane: events.LaneNormal}
	_, err := c.log.Append(context.Background(), []events.Event{e})
	return err
}

// start is one follower begun over the shared log: a new EventSpring, its
// first Spring, and the wait for the passes that Spring began.
func (c *eventClockRestartContext) start() error {
	spring := &application.EventSpring{Log: c.log, Host: "laptop", Jobs: c.jobs, Now: func() time.Time { return c.now }}
	if err := spring.Spring(context.Background()); err != nil {
		return err
	}
	spring.Wait()
	return nil
}

func (c *eventClockRestartContext) restarts(times, apart int) error {
	for i := 0; i < times; i++ {
		if i > 0 {
			c.now = c.now.Add(time.Duration(apart) * time.Minute)
		}
		if err := c.start(); err != nil {
			return err
		}
	}
	return nil
}

// otherEventsSince buries the log's last pass under n events of no job.
func (c *eventClockRestartContext) otherEventsSince(n int) error {
	evs := make([]events.Event, n)
	for i := range evs {
		evs[i] = events.Event{Ts: c.now, Kind: events.KindBeadChanged, Lane: events.LaneNormal, Actor: "mayor@laptop", Bead: "mw-other", From: "running", To: "running", Detail: "comment"}
	}
	_, err := c.log.Append(context.Background(), evs)
	return err
}

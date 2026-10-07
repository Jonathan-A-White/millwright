package steps

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

const (
	springMillKey  = "02" + "aa"
	springOtherKey = "02" + "bb"
)

// springMillKeyFile is the mill key file of features/event_grist_spring.feature.
type springMillKeyFile struct{}

func (springMillKeyFile) PublicKey() (string, string, error) { return springMillKey, "", nil }

// eventGristSpringContext is a follower whose springer holds the grist job,
// when the host has a [grist] table, over a fake postern backend; a pass of
// the mill is counted and, when the scenario says so, held until let go.
type eventGristSpringContext struct {
	postern *apptest.FakePostern
	log     *apptest.FakeEventLog
	spring  *application.EventSpring
	follow  application.EventFollow
	ctx     context.Context
	cancel  context.CancelFunc
	records int

	began   chan struct{}
	release chan struct{}
	ended   chan struct{}
	started int
	held    bool
	done    int
}

// InitializeEventGristSpringScenario registers the steps of features/event_grist_spring.feature.
func InitializeEventGristSpringScenario(ctx *godog.ScenarioContext) {
	c := &eventGristSpringContext{}

	ctx.Given(`^the event follower springs the mill on a host with a \[grist\] table$`, func() error { return c.setUp(true) })
	ctx.Given(`^the event follower springs the mill on a host with no \[grist\] table$`, func() error { return c.setUp(false) })
	ctx.Given(`^a grist record for the mill key waits at the postern backend$`, func() error {
		if c.postern == nil {
			c.postern = apptest.NewFakePostern()
		}
		c.addRecord(application.GristClass, springMillKey)
		return nil
	})
	ctx.Given(`^the mill's pass is held running$`, func() error { c.held = true; return nil })
	ctx.When(`^a grist record for the mill key reaches the postern backend$`, func() error { c.addRecord(application.GristClass, springMillKey); return nil })
	ctx.When(`^a grist record for another key reaches the postern backend$`, func() error { c.addRecord(application.GristClass, springOtherKey); return nil })
	ctx.When(`^a message record for the mill key reaches the postern backend$`, func() error { c.addRecord("message", springMillKey); return nil })
	ctx.When(`^the follower runs a cycle$`, c.cycle)
	ctx.When(`^the mill's pass is let go$`, func() error {
		c.held = false
		close(c.release)
		for c.done < c.started {
			select {
			case <-c.ended:
				c.done++
			case <-time.After(5 * time.Second):
				return fmt.Errorf("the mill's pass did not end")
			}
		}
		return c.cycle()
	})
	ctx.Then(`^the mill has made (\d+) passe?s?$`, func(n int) error { return c.wantStarted(n) })
	ctx.Then(`^the mill has begun (\d+) passe?s?$`, func(n int) error { return c.wantStarted(n) })
	ctx.After(func(context.Context, *godog.Scenario, error) (context.Context, error) {
		if c.cancel != nil {
			c.cancel()
		}
		if c.spring != nil {
			c.spring.Wait()
		}
		return nil, nil
	})
}

func (c *eventGristSpringContext) setUp(table bool) error {
	if c.postern == nil {
		c.postern = apptest.NewFakePostern()
	}
	c.log = &apptest.FakeEventLog{}
	c.began = make(chan struct{}, 16)
	c.release = make(chan struct{})
	c.ended = make(chan struct{}, 16)
	var jobs []application.SpringJob
	if table {
		watch := &application.GristWatch{Postern: c.postern, Keys: springMillKeyFile{}, Every: time.Nanosecond}
		jobs = append(jobs, application.GristJob(watch, func(context.Context) error {
			c.began <- struct{}{}
			if c.held {
				<-c.release
			}
			c.ended <- struct{}{}
			return nil
		}))
	}
	c.spring = &application.EventSpring{Log: c.log, Host: "laptop", Jobs: jobs}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	// The first cycle only reads where the backend stands.
	return c.cycle()
}

func (c *eventGristSpringContext) addRecord(class, to string) {
	if c.postern == nil {
		c.postern = apptest.NewFakePostern()
	}
	c.records++
	c.postern.AddRecord(application.PosternRecord{Txid: fmt.Sprintf("direct:%d", c.records), Class: class, To: to})
}

// cycle is one pass of the follower: the beads are quiet, so it only springs.
func (c *eventGristSpringContext) cycle() error {
	follow := application.EventFollow{
		Head:     &apptest.FakeHead{Heads: []string{"quiet"}},
		Feed:     apptest.NewFakeTracker(),
		Log:      c.log,
		Cursors:  &apptest.FakeFollowCursors{},
		Springer: c.spring,
		Publish:  func(context.Context) error { return nil },
		Sleep:    func(context.Context, time.Duration) error { return errors.New("one cycle") },
	}
	if err := follow.Run(c.ctx); err != nil {
		return fmt.Errorf("the follower ended with %w", err)
	}
	// A pass the cycle began runs in a goroutine: give it a moment to show.
	c.drain(150 * time.Millisecond)
	return nil
}

// drain counts the passes begun, waiting up to wait for the first.
func (c *eventGristSpringContext) drain(wait time.Duration) {
	timer := time.After(wait)
	for {
		select {
		case <-c.began:
			c.started++
		case <-timer:
			return
		}
	}
}

func (c *eventGristSpringContext) wantStarted(n int) error {
	if c.started != n {
		return fmt.Errorf("the mill made %d passes, want %d", c.started, n)
	}
	return nil
}

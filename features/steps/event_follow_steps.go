package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// eventFollowContext is the follower of features/event_follow.feature: a
// fake tracker's beads, an event log and a cursor in memory, and a clock the
// scenario moves on a second for every comment it adds.
type eventFollowContext struct {
	tracker *apptest.FakeTracker
	log     *apptest.FakeEventLog
	cursors *apptest.FakeFollowCursors
	t0      time.Time
	bead    application.BeadNow
	comment int
	said    string
}

// InitializeEventFollowScenario registers the steps of features/event_follow.feature.
func InitializeEventFollowScenario(ctx *godog.ScenarioContext) {
	c := &eventFollowContext{}

	ctx.Given(`^the event follower watches the closed bead "([^"]*)"$`, func(id string) error {
		*c = eventFollowContext{
			tracker: apptest.NewFakeTracker(),
			log:     &apptest.FakeEventLog{},
			cursors: &apptest.FakeFollowCursors{},
			t0:      time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC),
			bead:    application.BeadNow{ID: id, Type: "task", Status: application.StatusClosed},
		}
		c.tracker.SetBeadStates(c.t0, c.bead)
		// The first pass seeds the cursor and appends nothing.
		return c.pass("a")
	})
	ctx.When(`^the bead "([^"]*)" gains the comment "([^"]*)"$`, func(id, text string) error {
		c.comment++
		c.tracker.AddBeadChange(application.BeadChange{
			Key: fmt.Sprintf("c%d", c.comment), At: c.t0.Add(time.Duration(c.comment) * time.Second),
			Actor: "mw@laptop", What: application.ChangeComment, Comment: text,
			Bead: application.BeadNow{ID: id, Type: "task", Status: application.StatusClosed},
		})
		return c.pass(fmt.Sprintf("head%d", c.comment))
	})
	ctx.Then(`^the event follower's log holds "([^"]*)"$`, func(want string) error {
		var got []string
		for _, e := range c.log.All() {
			got = append(got, fmt.Sprintf("%s %s %s->%s %s", e.Kind, e.Bead, e.From, e.To, e.Detail))
		}
		if have := strings.Join(got, ", "); have != want {
			return fmt.Errorf("the log holds %q, want %q (the follower said %q)", have, want, c.said)
		}
		return nil
	})
}

// pass runs the follower for one pass over the head it is given.
func (c *eventFollowContext) pass(head string) error {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var errs bytes.Buffer
	follow := application.EventFollow{
		Head:    &apptest.FakeHead{Heads: []string{head}},
		Feed:    c.tracker,
		Log:     c.log,
		Cursors: c.cursors,
		Publish: func(context.Context) error { return nil },
		Now:     func() time.Time { return c.t0.Add(time.Hour) },
		Sleep:   func(context.Context, time.Duration) error { stop(); return ctx.Err() },
		Err:     &errs,
	}
	if err := follow.Run(ctx); err != nil {
		return fmt.Errorf("the follower ended with %w", err)
	}
	c.said += errs.String()
	return nil
}

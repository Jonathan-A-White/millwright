package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// stampContext is features/stamp.feature: rigs with fake heads, a fake stamp
// queue, and what mw stamp printed. Built by the first Given, never in a Before
// hook, which would run for every feature's scenarios.
type stampContext struct {
	rigs  map[string]string
	heads *apptest.FakeRigHeads
	queue *apptest.FakeStampQueue
	out   bytes.Buffer
	err   error
	ran   bool
}

// InitializeStampScenario registers the steps of features/stamp.feature.
func InitializeStampScenario(ctx *godog.ScenarioContext) {
	c := &stampContext{}

	ctx.Given(`^the rigs "([^"]*)" and "([^"]*)" have heads "([^"]*)" and "([^"]*)"$`, c.theRigs)
	ctx.Given(`^the rig "([^"]*)" has an older commit "([^"]*)"$`, func(rig, commit string) error {
		c.heads.AddCommit(c.rigs[rig], commit)
		return nil
	})
	ctx.Given(`^the head of "([^"]*)" was stamped as "([^"]*)"$`, c.theHeadWasStamped)
	ctx.When(`^I stamp the rig "([^"]*)"$`, func(rig string) error { return c.iStamp(rig, "", false) })
	ctx.When(`^I stamp the rig "([^"]*)" at the commit "([^"]*)"$`, func(rig, commit string) error {
		return c.iStamp(rig, commit, false)
	})
	ctx.When(`^I stamp every rig$`, func() error { return c.iStamp("", "", true) })
	ctx.Then(`^the stamp succeeded$`, func() error {
		if c.err != nil {
			return fmt.Errorf("the stamp failed: %v", c.err)
		}
		return nil
	})
	ctx.Then(`^the stamp failed with "([^"]*)"$`, func(want string) error {
		if c.err == nil || c.err.Error() != want {
			return fmt.Errorf("the stamp's error was %v, want %q", c.err, want)
		}
		return nil
	})
	ctx.Then(`^the stamp output says "([^"]*)"$`, func(want string) error {
		if !strings.Contains(c.out.String(), want) {
			return fmt.Errorf("the output does not say %q:\n%s", want, c.out.String())
		}
		return nil
	})
	ctx.Then(`^(\d+) stamps? (?:is|are) queued$`, func(n int) error {
		if got := len(c.queue.Queued()); got != n {
			return fmt.Errorf("%d stamps are queued, want %d", got, n)
		}
		return nil
	})
	ctx.Then(`^(\d+) stamps? (?:is|are) queued: rig "([^"]*)" commit "([^"]*)" with no story and title "([^"]*)"$`, c.queuedStamp)
}

func (c *stampContext) theRigs(a, b, headA, headB string) error {
	*c = stampContext{
		rigs:  map[string]string{a: "/rigs/" + a, b: "/rigs/" + b},
		heads: apptest.NewFakeRigHeads(),
		queue: apptest.NewFakeStampQueue(),
	}
	c.heads.SetHead(c.rigs[a], headA)
	c.heads.SetHead(c.rigs[b], headB)
	return nil
}

func (c *stampContext) theHeadWasStamped(rig, txid string) error {
	commit, err := c.heads.Resolve(context.Background(), c.rigs[rig], "")
	if err != nil {
		return err
	}
	stamp := domain.Stamp{Rig: rig, Branch: "main", Commit: commit.Commit, Host: "laptop",
		At: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)}
	ctx := context.Background()
	if err := c.queue.Append(ctx, stamp); err != nil {
		return err
	}
	return c.queue.MarkSent(ctx, stamp, txid, stamp.At)
}

func (c *stampContext) iStamp(rig, commit string, all bool) error {
	c.err = application.StampHead{
		Rigs: c.rigs, Heads: c.heads, Queue: c.queue, Store: c.queue, Host: "laptop", Out: &c.out,
		Now: func() time.Time { return time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC) },
	}.Run(context.Background(), rig, commit, all)
	return nil
}

func (c *stampContext) queuedStamp(n int, rig, commit, title string) error {
	queued := c.queue.Queued()
	if len(queued) != n {
		return fmt.Errorf("%d stamps are queued, want %d", len(queued), n)
	}
	got := queued[len(queued)-1].Stamp
	if got.Rig != rig || got.Commit != commit || got.Story != "" || got.Title != title || got.Host != "laptop" {
		return fmt.Errorf("the queued stamp is %+v, want rig %q commit %q no story title %q", got, rig, commit, title)
	}
	return nil
}

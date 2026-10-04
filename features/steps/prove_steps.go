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

// proveContext is features/prove.feature: a fake stamp queue holding what was
// sent, a fake chain lookup, and what mw prove printed. Built by the first
// Given, never in a Before hook, which would run for every feature's scenarios.
type proveContext struct {
	queue *apptest.FakeStampQueue
	chain *apptest.FakeChainLookup
	out   bytes.Buffer
	err   error
}

// InitializeProveScenario registers the steps of features/prove.feature.
func InitializeProveScenario(ctx *godog.ScenarioContext) {
	c := &proveContext{}

	ctx.Given(`^the rig "([^"]*)" has a commit "([^"]*)" that was stamped as "([^"]*)"$`, c.aStampedCommit)
	ctx.Given(`^the chain has "([^"]*)" in block (\d+) at "([^"]*)"$`, c.theChainHasABlock)
	ctx.Given(`^the chain has "([^"]*)" in the mempool$`, func(txid string) error {
		c.chain.Mempool(txid)
		return nil
	})
	ctx.When(`^I prove "([^"]*)" "([^"]*)"$`, c.iProve)
	ctx.Then(`^the proof succeeded$`, func() error {
		if c.err != nil {
			return fmt.Errorf("the proof failed: %v", c.err)
		}
		return nil
	})
	ctx.Then(`^the proof failed with "([^"]*)"$`, func(want string) error {
		if c.err == nil || c.err.Error() != want {
			return fmt.Errorf("the proof's error was %v, want %q", c.err, want)
		}
		return nil
	})
	ctx.Then(`^the proof says "([^"]*)"$`, func(want string) error {
		if !strings.Contains(c.out.String(), want) {
			return fmt.Errorf("the proof does not say %q:\n%s", want, c.out.String())
		}
		return nil
	})
}

func (c *proveContext) aStampedCommit(rig, commit, txid string) error {
	*c = proveContext{queue: apptest.NewFakeStampQueue(), chain: apptest.NewFakeChainLookup()}
	stamp := domain.Stamp{
		Rig: rig, Branch: "main", Commit: commit, Story: "mw-a.1", Title: "A story",
		Host: "laptop", At: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC),
	}
	ctx := context.Background()
	if err := c.queue.Append(ctx, stamp); err != nil {
		return err
	}
	return c.queue.MarkSent(ctx, stamp, txid, time.Date(2026, 10, 4, 9, 1, 0, 0, time.UTC))
}

func (c *proveContext) theChainHasABlock(txid string, height int, at string) error {
	when, err := time.Parse("2006-01-02 15:04:05", at)
	if err != nil {
		return err
	}
	c.chain.Confirm(txid, int64(height), when)
	return nil
}

func (c *proveContext) iProve(rig, commit string) error {
	c.out.Reset()
	c.err = application.Prove{Stamps: c.queue, Chain: c.chain, Out: &c.out}.Run(context.Background(), rig, commit)
	return nil
}

package steps

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerStatusRoomSteps registers the steps of features/status.feature that
// are about the room a host has to start a story (mw-t0z3fu.1). They are called
// after the Before hook, so the context they fill is the scenario's own.
func registerStatusRoomSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^this host runs at most (\d+) sessions at once$`, c.thisHostRunsAtMost)
	ctx.Given(`^this host is at load ([0-9.]+) of (\d+) cores with (\d+) MB of memory available$`, c.thisHostIsAtLoadWithMemory)
	ctx.Given(`^this host's load cannot be read for the report$`, c.thisHostsLoadCannotBeReadForTheReport)
	ctx.Given(`^this host's mill answered (\d+) tutor-turn grists in (\d+) seconds each long ago, then (\d+) in (\d+) seconds each in the last 5 minutes$`, c.thisHostsMillAnswered)
	ctx.Then(`^the report does not say "([^"]*)"$`, c.theReportDoesNotSay)
	ctx.Then(`^the report says "([^"]*)"$`, c.theReportSays)
	ctx.Then(`^the report says the host is held back: "([^"]*)"$`, c.theReportSaysTheHostIsHeldBack)
	ctx.Then(`^the report does not say the host is held back$`, c.theReportDoesNotSayTheHostIsHeldBack)
}

func (c *statusContext) thisHostRunsAtMost(n int) error {
	c.cap = n
	return nil
}

func (c *statusContext) thisHostIsAtLoadWithMemory(load string, cores int, mb int64) error {
	value, err := strconv.ParseFloat(load, 64)
	if err != nil {
		return fmt.Errorf("%q is not a load: %w", load, err)
	}
	c.load = &apptest.FakeHostLoad{Reading: application.LoadReading{Load: value, Cores: cores, MemAvailableMB: mb, MemKnown: true}}
	return nil
}

func (c *statusContext) thisHostsLoadCannotBeReadForTheReport() error {
	c.load = &apptest.FakeHostLoad{Err: errors.New("no /proc/loadavg")}
	return nil
}

func (c *statusContext) theReportSays(text string) error {
	if printed := c.report.String(); !strings.Contains(printed, text) {
		return fmt.Errorf("expected the report to say %q, got:\n%s", text, printed)
	}
	return nil
}

func (c *statusContext) theReportSaysTheHostIsHeldBack(why string) error {
	for _, part := range strings.Split(why, "; ") {
		if err := c.theReportSays("held back, no room: " + part); err != nil {
			return err
		}
	}
	return nil
}

func (c *statusContext) theReportDoesNotSayTheHostIsHeldBack() error {
	if printed := c.report.String(); strings.Contains(printed, "held back") {
		return fmt.Errorf("expected the report not to say the host is held back, got:\n%s", printed)
	}
	return nil
}

// thisHostsMillAnswered gives the status the mill's runs: n grists of old
// seconds that ended hours ago and then recent of seconds each that ended in
// the last five minutes.
func (c *statusContext) thisHostsMillAnswered(n, old, recent, seconds int) error {
	c.gristRuns = &apptest.FakeGristRuns{}
	add := func(ago time.Duration, secs int) {
		answered := c.now.Add(-ago)
		run := float64(secs - 2)
		received := answered.Add(-time.Duration(run) * time.Second)
		sent := received.Add(-2 * time.Second)
		queued := 2.0
		c.gristRuns.Kept = append(c.gristRuns.Kept, application.GristRunTiming{
			Txid: fmt.Sprintf("direct:tutor-%d", len(c.gristRuns.Kept)), Kind: "tutor-turn",
			Sent: &sent, Received: received, Answered: answered, Seconds: run, QueuedSeconds: &queued, RunSeconds: run,
		})
	}
	for i := 0; i < n; i++ {
		add(3*time.Hour+time.Duration(n-i)*time.Second, old)
	}
	for i := 0; i < recent; i++ {
		add(time.Duration(recent-i)*time.Minute, seconds)
	}
	return nil
}

func (c *statusContext) theReportDoesNotSay(text string) error {
	if printed := c.report.String(); strings.Contains(printed, text) {
		return fmt.Errorf("expected the report not to say %q, got:\n%s", text, printed)
	}
	return nil
}

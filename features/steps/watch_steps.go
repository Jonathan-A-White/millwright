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

// watchContext holds a fake world for mw watch to look at — which URLs answer,
// what ssh prints — and a fake tracker for the sync note. Nothing here reaches
// the network, ssh, the factory's own beads database or this host's own
// watch memory and log.
type watchContext struct {
	probes  *apptest.FakeWatch
	tracker *apptest.FakeTracker

	settings application.WatchSettings
	now      time.Time

	said   bytes.Buffer
	report application.WatchReport
	err    error
}

// InitializeWatchScenario registers the steps of features/watch.feature.
func InitializeWatchScenario(ctx *godog.ScenarioContext) {
	c := &watchContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = watchContext{
			probes:  apptest.NewFakeWatch(),
			tracker: apptest.NewFakeTracker(),
			now:     time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})

	// A step that sets the world up, or runs the command, reads the same after a
	// Given and after a When, and an And takes the keyword above it.
	both := func(pattern string, step interface{}) {
		ctx.Given(pattern, step)
		ctx.When(pattern, step)
	}

	ctx.Given(`^a watch of the host "([^"]*)" over ssh "([^"]*)", with the outside places "([^"]*)" and "([^"]*)" and the blog "([^"]*)"$`, c.aWatchOf)
	ctx.Given(`^the config has no watch table$`, c.theConfigHasNoWatchTable)
	ctx.Given(`^the watch clock reads "([^"]*)"$`, c.theWatchClockReads)

	both(`^the outside places answer$`, c.theOutsidePlacesAnswer)
	both(`^neither outside place answers$`, c.neitherOutsidePlaceAnswers)
	both(`^the outside places stop answering$`, c.neitherOutsidePlaceAnswers)
	both(`^only "([^"]*)" answers of the outside places$`, c.onlyOneOutsidePlaceAnswers)
	both(`^the blog answers$`, c.theBlogAnswers)
	both(`^the blog does not answer$`, c.theBlogDoesNotAnswer)
	both(`^the ssh read fails$`, c.theSSHReadFails)
	both(`^the health line of the watched host is (\d+) minutes? old and ends "([^"]*)"$`, c.theHealthLineIsMinutesOld)
	both(`^the health line of the watched host is "([^"]*)"$`, c.theHealthLineIs)
	both(`^the watched host last synced (\d+) minutes ago$`, c.theWatchedHostLastSynced)
	both(`^(\d+) minutes? pass$`, c.minutesPass)
	both(`^mw watch runs$`, c.mwWatchRuns)

	ctx.Then(`^mw watch says "([^"]*)"$`, c.mwWatchSays)
	ctx.Then(`^mw watch leaves with the status (\d+)$`, c.mwWatchLeavesWith)
	ctx.Then(`^ssh was not tried$`, c.sshWasNotTried)
	ctx.Then(`^nothing outside was tried and ssh was not tried$`, c.nothingWasTried)
	ctx.Then(`^ssh was asked to read the health line of "([^"]*)" once$`, c.sshWasAskedOnce)
	ctx.Then(`^the watch memory was never touched$`, c.theMemoryWasNeverTouched)
	ctx.Then(`^the watch memory holds the first failure at "([^"]*)"$`, c.theMemoryHoldsTheFirstFailure)
	ctx.Then(`^the watch memory holds no failure$`, c.theMemoryHoldsNoFailure)
	ctx.Then(`^the watch log holds exactly (\d+) lines?$`, c.theLogHolds)
	ctx.Then(`^the last watch log line is "([^"]*)"$`, c.theLastLogLineIs)
}

func (c *watchContext) aWatchOf(host, ssh, one, two, blog string) error {
	c.settings = application.WatchSettings{SSH: ssh, Host: host, Outside: []string{one, two}, Blog: blog}
	return nil
}

func (c *watchContext) theConfigHasNoWatchTable() error {
	c.settings = application.WatchSettings{}
	return nil
}

func (c *watchContext) theWatchClockReads(at string) error {
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return fmt.Errorf("%q is not a time: %w", at, err)
	}
	c.now = when
	return nil
}

func (c *watchContext) theOutsidePlacesAnswer() error {
	for _, url := range c.settings.Outside {
		c.probes.Answering[url] = true
	}
	return nil
}

func (c *watchContext) neitherOutsidePlaceAnswers() error {
	for _, url := range c.settings.Outside {
		c.probes.Answering[url] = false
	}
	return nil
}

func (c *watchContext) onlyOneOutsidePlaceAnswers(only string) error {
	for _, url := range c.settings.Outside {
		c.probes.Answering[url] = url == only
	}
	return nil
}

func (c *watchContext) theBlogAnswers() error {
	c.probes.Answering[c.settings.Blog] = true
	return nil
}

func (c *watchContext) theBlogDoesNotAnswer() error {
	c.probes.Answering[c.settings.Blog] = false
	return nil
}

func (c *watchContext) theSSHReadFails() error {
	c.probes.SSHFails = true
	return nil
}

// theHealthLineIsMinutesOld makes ssh print a line as mw-health.sh writes it,
// stamped that long before the clock and ending in the verdict given.
func (c *watchContext) theHealthLineIsMinutesOld(minutes int, verdict string) error {
	at := c.now.Add(-time.Duration(minutes) * time.Minute).UTC().Format(time.RFC3339)
	return c.theHealthLineIs(at + " load1=0.50 mem_avail_mb=1000 disk_pct=42 services=none " + verdict)
}

func (c *watchContext) theHealthLineIs(line string) error {
	c.probes.SSHFails = false
	c.probes.Health = line + "\n"
	return nil
}

func (c *watchContext) theWatchedHostLastSynced(minutes int) error {
	at := c.now.Add(-time.Duration(minutes) * time.Minute).UTC().Format(application.LastSyncFormat)
	return c.tracker.SetNote(context.Background(), application.LastSyncKey(c.settings.Host), at)
}

func (c *watchContext) minutesPass(minutes int) error {
	c.now = c.now.Add(time.Duration(minutes) * time.Minute)
	return nil
}

func (c *watchContext) mwWatchRuns() error {
	c.said.Reset()
	c.report, c.err = application.Watch{
		Probes:   c.probes,
		Notes:    c.tracker,
		Settings: c.settings,
		Now:      func() time.Time { return c.now },
		Out:      &c.said,
	}.Run(context.Background())
	if c.err != nil {
		if _, wake := application.WatchWakes(c.err); !wake {
			return fmt.Errorf("mw watch failed: %w", c.err)
		}
	}
	return nil
}

func (c *watchContext) mwWatchSays(line string) error {
	if got := strings.TrimSuffix(c.said.String(), "\n"); got != line || strings.Count(c.said.String(), "\n") != 1 {
		return fmt.Errorf("expected mw watch to print the one line %q, it printed %q", line, c.said.String())
	}
	return nil
}

func (c *watchContext) mwWatchLeavesWith(status int) error {
	if got := application.ExitStatus(c.err); got != status {
		return fmt.Errorf("expected mw watch to leave with %d, it leaves with %d (%v)", status, got, c.err)
	}
	return nil
}

func (c *watchContext) sshWasNotTried() error {
	if reads := c.probes.Reads(); len(reads) > 0 {
		return fmt.Errorf("expected ssh not to be tried, it was asked %d time(s)", len(reads))
	}
	return nil
}

func (c *watchContext) nothingWasTried() error {
	if reached := c.probes.Reached(); len(reached) > 0 {
		return fmt.Errorf("expected nothing outside to be tried, these were: %v", reached)
	}
	return c.sshWasNotTried()
}

func (c *watchContext) sshWasAskedOnce(ssh string) error {
	if reads := c.probes.Reads(); len(reads) != 1 || reads[0] != ssh {
		return fmt.Errorf("expected exactly one health read of %q, got %v", ssh, reads)
	}
	return nil
}

func (c *watchContext) theMemoryWasNeverTouched() error {
	if saves := c.probes.Saves(); saves > 0 {
		return fmt.Errorf("expected the watch memory never to be written, it was written %d time(s)", saves)
	}
	return nil
}

func (c *watchContext) theMemoryHoldsTheFirstFailure(at string) error {
	if got := c.probes.FirstFailure().UTC().Format(time.RFC3339); got != at {
		return fmt.Errorf("expected the first failure remembered at %s, the memory holds %q", at, got)
	}
	return nil
}

func (c *watchContext) theMemoryHoldsNoFailure() error {
	if first := c.probes.FirstFailure(); !first.IsZero() {
		return fmt.Errorf("expected no failure remembered, the memory holds one at %s", first.UTC().Format(time.RFC3339))
	}
	return nil
}

func (c *watchContext) theLogHolds(n int) error {
	if got := c.probes.Log(); len(got) != n {
		return fmt.Errorf("expected the watch log to hold %d line(s), it holds %d: %q", n, len(got), got)
	}
	return nil
}

func (c *watchContext) theLastLogLineIs(line string) error {
	log := c.probes.Log()
	if len(log) == 0 {
		return fmt.Errorf("expected the watch log to end in %q, it is empty", line)
	}
	if got := log[len(log)-1]; got != line {
		return fmt.Errorf("expected the watch log to end in %q, it ends in %q", line, got)
	}
	return nil
}

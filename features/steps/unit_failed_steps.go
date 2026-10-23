package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// unitFailedContext holds the fake init system, the failures it keeps and the
// mailbox of features/unit_failed.feature, and a clock that moves a minute
// with every step that changes a unit.
type unitFailedContext struct {
	units    *apptest.FakeUnitSystem
	failures *apptest.FakeUnitFailures
	mailbox  *apptest.FakeMailbox
	now      time.Time
	err      error
}

// InitializeUnitFailedScenario registers the steps of features/unit_failed.feature.
func InitializeUnitFailedScenario(ctx *godog.ScenarioContext) {
	c := &unitFailedContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = unitFailedContext{
			units:    apptest.NewFakeUnitSystem(),
			failures: apptest.NewFakeUnitFailures(),
			mailbox:  apptest.NewFakeMailbox(),
			now:      time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC),
		}
		return ctx, nil
	})

	ctx.Given(`^the unit "([^"]*)" is run by a timer$`, func(unit string) error { c.units.SetTimer(unit, true); return nil })
	ctx.Given(`^the unit "([^"]*)" is run by no timer$`, func(unit string) error { c.units.SetTimer(unit, false); return nil })
	ctx.Given(`^the unit "([^"]*)" failed with the journal "([^"]*)"$`, c.fails)
	ctx.Given(`^the unit "([^"]*)" then ran clean$`, c.ranClean)

	ctx.When(`^the unit "([^"]*)" fails$`, func(unit string) error { return c.fails(unit, "the journal's last lines") })
	ctx.When(`^the unit "([^"]*)" fails with the journal "([^"]*)"$`, c.fails)

	ctx.Then(`^no mail was sent to the Mayor$`, func() error { return c.mailCount(0) })
	ctx.Then(`^one mail was sent to the Mayor$`, func() error { return c.mailCount(1) })
	ctx.Then(`^the mail subject is "([^"]*)"$`, c.subjectIs)
	ctx.Then(`^the mail is from "([^"]*)"$`, c.fromIs)
	ctx.Then(`^the mail body says "([^"]*)"$`, c.bodySays)
	ctx.Then(`^one failure of "([^"]*)" is recorded$`, func(unit string) error { return c.recorded(unit, true) })
	ctx.Then(`^no failure of "([^"]*)" is recorded$`, func(unit string) error { return c.recorded(unit, false) })
	ctx.Then(`^the recorded journal of "([^"]*)" says "([^"]*)"$`, c.recordedJournalSays)
}

func (c *unitFailedContext) tick() time.Time {
	c.now = c.now.Add(time.Minute)
	return c.now
}

func (c *unitFailedContext) fails(unit, journal string) error {
	c.units.SetJournal(unit, journal)
	_, c.err = application.UnitFailed{
		Units: c.units, Failures: c.failures, Mailbox: c.mailbox, Host: "laptop", Now: c.tick,
	}.Run(context.Background(), unit)
	return nil
}

func (c *unitFailedContext) ranClean(unit string) error {
	c.units.RanClean(unit, c.tick())
	return nil
}

func (c *unitFailedContext) sent() ([]application.Message, error) {
	if c.err != nil {
		return nil, fmt.Errorf("unit-failed failed: %w", c.err)
	}
	return c.mailbox.Inbox(context.Background(), application.MayorMailbox)
}

func (c *unitFailedContext) mailCount(want int) error {
	sent, err := c.sent()
	if err != nil {
		return err
	}
	if len(sent) != want {
		return fmt.Errorf("expected %d mail to the Mayor, got %d: %+v", want, len(sent), sent)
	}
	return nil
}

func (c *unitFailedContext) onlyMail() (application.Message, error) {
	sent, err := c.sent()
	if err != nil {
		return application.Message{}, err
	}
	if len(sent) != 1 {
		return application.Message{}, fmt.Errorf("expected one mail to the Mayor, got %d", len(sent))
	}
	return sent[0], nil
}

func (c *unitFailedContext) subjectIs(want string) error {
	m, err := c.onlyMail()
	if err != nil {
		return err
	}
	if m.Subject != want {
		return fmt.Errorf("the subject is %q, want %q", m.Subject, want)
	}
	return nil
}

func (c *unitFailedContext) fromIs(want string) error {
	m, err := c.onlyMail()
	if err != nil {
		return err
	}
	if m.From != want {
		return fmt.Errorf("the mail is from %q, want %q", m.From, want)
	}
	return nil
}

func (c *unitFailedContext) bodySays(want string) error {
	m, err := c.onlyMail()
	if err != nil {
		return err
	}
	if !strings.Contains(m.Body, want) {
		return fmt.Errorf("the body does not say %q:\n%s", want, m.Body)
	}
	return nil
}

func (c *unitFailedContext) recorded(unit string, want bool) error {
	_, ok, err := c.failures.Load(context.Background(), unit)
	if err != nil {
		return err
	}
	if ok != want {
		return fmt.Errorf("a failure of %s recorded = %v, want %v", unit, ok, want)
	}
	return nil
}

func (c *unitFailedContext) recordedJournalSays(unit, want string) error {
	failure, ok, err := c.failures.Load(context.Background(), unit)
	if err != nil {
		return err
	}
	if !ok || !strings.Contains(failure.Journal, want) {
		return fmt.Errorf("the recorded journal of %s is %q (recorded: %v), want it to say %q", unit, failure.Journal, ok, want)
	}
	return nil
}

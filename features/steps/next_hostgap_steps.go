package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// The steps of the scenarios that say a failure of the failing tests on main
// alone is one host's, not main's (mw-gq6.1395): the home is asked to run them by
// name before main is called red, and a fake stands for its answer.

func registerNextHostGapSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the vault names "([^"]*)" as the home$`, func(host string) error {
		c.home = homeFile(host + " 2026-10-01T05:00:00Z mayor\n")
		return nil
	})
	ctx.Given(`^the home "([^"]*)" passes the failing tests when they are run by name there$`, func(host string) error {
		c.namedElsewhere().Runs(host, application.NamedRun{Ran: true, Checked: application.Checked{Passed: true, Command: "npm test -- ffmpeg"}})
		return nil
	})
	ctx.Given(`^the home "([^"]*)" fails the failing tests when they are run by name there, saying "([^"]*)"$`, func(host, saying string) error {
		c.namedElsewhere().Runs(host, application.NamedRun{Ran: true, Checked: application.Checked{Output: saying, Command: "npm test -- ffmpeg"}})
		return nil
	})
	ctx.Given(`^the home "([^"]*)" cannot be reached$`, func(host string) error {
		c.namedElsewhere().Unreachable(host, errors.New("ssh: connect to host "+host+": Connection timed out"))
		return nil
	})
	ctx.Then(`^"([^"]*)" was asked to run by name: (.+)$`, func(host, given string) error {
		asked := c.namedElsewhere().Asked()
		if len(asked) != 1 || asked[0].Host != host || strings.Join(asked[0].Tests, " ") != strings.TrimSpace(given) {
			return fmt.Errorf("expected %s to be asked once to run %q, got %+v", host, given, asked)
		}
		return nil
	})
	ctx.Then(`^no other host was asked to run the failing tests$`, func() error {
		if asked := c.namedElsewhere().Asked(); len(asked) != 0 {
			return fmt.Errorf("expected no other host to be asked, got %+v", asked)
		}
		return nil
	})
	ctx.Then(`^the story "([^"]*)" is held as a host gap of "([^"]*)", waiting on a new held bug pathed to that host naming "([^"]*)"$`, c.theStoryIsHeldAsAHostGap)
	ctx.Then(`^no bug for main's red was filed$`, func() error {
		bugs, err := c.mainRedBugs()
		if err != nil {
			return err
		}
		if len(bugs) != 0 {
			return fmt.Errorf("expected no main-red bug, got %+v", bugs)
		}
		return nil
	})
	ctx.Then(`^the report says "([^"]*)" was held as a host gap, not as main's red$`, func(string) error {
		if c.report.MainRed == nil || c.report.MainRed.Gap == nil || c.report.Reason != application.ReasonHostGap || c.report.SentBack != "" || c.report.Refused {
			return fmt.Errorf("expected the report to say the story was held as a host gap, got %+v", c.report)
		}
		if want := "HELD (host-gap) host gap: "; !strings.Contains(c.printed.String(), want) {
			return fmt.Errorf("expected the printed report to say %q, got:\n%s", want, c.printed.String())
		}
		return nil
	})
}

func (c *nextContext) namedElsewhere() *apptest.FakeNamedElsewhere {
	if c.elsewhere == nil {
		c.elsewhere = apptest.NewFakeNamedElsewhere()
	}
	return c.elsewhere
}

// hostGapBugs are the bugs mw next filed for a host's gap, found by title.
func (c *nextContext) hostGapBugs() ([]application.StoryDetail, error) {
	all, err := c.tracker.StoriesSince(context.Background(), time.Time{})
	if err != nil {
		return nil, err
	}
	var found []application.StoryDetail
	for _, d := range all {
		if strings.HasPrefix(d.Story.Title, "[bug] host gap: ") {
			found = append(found, d)
		}
	}
	return found, nil
}

func (c *nextContext) theStoryIsHeldAsAHostGap(id, host, test string) error {
	bugs, err := c.hostGapBugs()
	if err != nil {
		return err
	}
	if len(bugs) != 1 || !strings.Contains(bugs[0].Story.Title+bugs[0].Description, test) || !strings.Contains(bugs[0].Story.Title, host) {
		return fmt.Errorf("expected one new [bug] of the gap of %s naming %s, got %+v", host, test, bugs)
	}
	if bugs[0].Status != "deferred" && bugs[0].Status != "held" {
		return fmt.Errorf("expected the new bug %s to be held, its status is %q", bugs[0].Story.ID, bugs[0].Status)
	}
	path, err := bugs[0].Path()
	if err != nil {
		return err
	}
	if path.Host != host {
		return fmt.Errorf("expected the bug %s to be pathed to host %s, it is pathed to %q", bugs[0].Story.ID, host, path.Host)
	}
	if !strings.Contains(bugs[0].Description, "no ffmpeg") {
		return fmt.Errorf("expected the bug to carry the error line %q, got: %s", "no ffmpeg", bugs[0].Description)
	}
	return c.theStoryIsHeldOnMainsRed(id, bugs[0].Story.ID)
}

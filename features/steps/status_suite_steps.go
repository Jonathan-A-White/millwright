package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// registerStatusSuiteSteps registers the steps of features/status.feature that
// are about the SUITE section (mw-8w7xb7.3). They are called after the Before
// hook, so the context they fill is the scenario's own.
func registerStatusSuiteSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^the status rig "([^"]*)" runs the nightly suite, its bugs filed under the epic "([^"]*)"$`, c.theStatusRigRunsTheNightlySuite)
	ctx.Given(`^the status rig "([^"]*)" does not run the nightly suite$`, c.theStatusRigDoesNot)
	ctx.Given(`^the epic "([^"]*)" holds the held bug "([^"]*)" for "([^"]*)" of "([^"]*)"$`, c.theEpicHoldsTheHeldBug)
	ctx.Given(`^the last suite of "([^"]*)" was kept (today|\d+ days? ago) at "([^"]*)", failing "([^"]*)" and "([^"]*)"$`, c.theLastSuiteFailed)
	ctx.Given(`^the last suite of "([^"]*)" was kept (today|\d+ days? ago) at "([^"]*)", all passed$`, c.theLastSuitePassed)
	ctx.Then(`^the report has no heading for the suite$`, c.theReportHasNoSuiteHeading)
}

func (c *statusContext) theStatusRigRunsTheNightlySuite(rig, epic string) error {
	c.suiteRigs(rig)
	c.tracker.AddEpic(epic, domain.Path{Rig: rig, Branch: "main", Harness: "claude", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "vps"})
	c.epicRules.Require(rig, domain.EpicRequirements{NightlySuite: true, SuiteEpic: epic})
	return nil
}

func (c *statusContext) theStatusRigDoesNot(rig string) error {
	c.suiteRigs(rig)
	c.epicRules.Require(rig, domain.EpicRequirements{})
	return nil
}

func (c *statusContext) suiteRigs(rig string) {
	if c.rigs == nil {
		c.rigs = map[string]string{}
		c.suites = &apptest.FakeSuiteRecords{}
	}
	c.rigs[rig] = "/rigs/" + rig
}

func (c *statusContext) theEpicHoldsTheHeldBug(epic, id, file, rig string) error {
	c.tracker.AddStory(epic, domain.Story{ID: id, Title: fmt.Sprintf("[bug] %s main: %s fails on main", rig, file)})
	if err := c.tracker.SetLabels(id, "bug", "suite"); err != nil {
		return err
	}
	return c.tracker.SetStatus(id, apptest.StatusDeferred)
}

// keptOn is the UTC day a result said to be kept "today" or "N days ago" is of.
func (c *statusContext) keptOn(when string) string {
	days := 0
	if when != "today" {
		fmt.Sscanf(when, "%d", &days)
	}
	return c.now.UTC().Add(-time.Duration(days) * 24 * time.Hour).Format("2006-01-02")
}

func (c *statusContext) keepSuite(rig, when, sha string, failing ...string) error {
	if c.suites == nil {
		return fmt.Errorf("the rig %q does not run the nightly suite in this scenario", rig)
	}
	var files []string
	for _, file := range failing {
		files = append(files, fmt.Sprintf(`{"file":%q,"phase":"tests","flaky":false,"alone":"failed"}`, file))
	}
	result := fmt.Sprintf(`{"rig":%q,"sha":%q,"passed":%t,"failing_files":[%s]}`, rig, sha, len(failing) == 0, strings.Join(files, ","))
	_, err := c.suites.PutSuiteFile(context.Background(), rig, c.keptOn(when)+".json", result)
	return err
}

func (c *statusContext) theLastSuiteFailed(rig, when, sha, first, second string) error {
	return c.keepSuite(rig, when, sha, first, second)
}

func (c *statusContext) theLastSuitePassed(rig, when, sha string) error {
	return c.keepSuite(rig, when, sha)
}

func (c *statusContext) theReportHasNoSuiteHeading() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if strings.Contains(c.report.String(), application.SuiteHeading) {
		return fmt.Errorf("expected no %q heading, got:\n%s", application.SuiteHeading, c.report.String())
	}
	return nil
}

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// registerStatusCauseSteps registers the steps of features/status.feature that
// are about the CAUSES MISSING section (mw-2zh33g.1). They are called after the
// Before hook, so the context they fill is the scenario's own.
func registerStatusCauseSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^the status story "([^"]*)" was refused (\d+) days? ago$`, c.theStatusStoryWasRefusedDaysAgo)
	ctx.Given(`^the status story "([^"]*)" has its cause recorded as "([^"]*)"$`, c.theStatusStoryHasItsCause)
	ctx.Then(`^the report lists "([^"]*)" under CAUSES MISSING$`, c.theReportListsUnderCausesMissing)
	ctx.Then(`^the report does not list "([^"]*)" under CAUSES MISSING$`, c.theReportDoesNotListUnderCausesMissing)
	ctx.Then(`^the report has no heading for causes missing$`, c.theReportHasNoCausesMissingHeading)
}

func (c *statusContext) theStatusStoryWasRefusedDaysAgo(id string, days int) error {
	note := fmt.Sprintf("mw next on vps %s%s): the rig's tests fail in the worktree", application.RefusedPhrase, application.ReasonTestsFail)
	return c.tracker.CommentOnStoryAt(id, note, c.now.Add(-time.Duration(days)*24*time.Hour))
}

func (c *statusContext) theStatusStoryHasItsCause(id, class string) error {
	return c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{application.CauseField: class})
}

func (c *statusContext) causesMissingSection() (string, error) {
	if err := c.readingStatusSucceeds(); err != nil {
		return "", err
	}
	return headedBy(c.report.String(), application.CausesMissingHeading), nil
}

func (c *statusContext) theReportListsUnderCausesMissing(id string) error {
	section, err := c.causesMissingSection()
	if err != nil {
		return err
	}
	if !strings.Contains(section, id) {
		return fmt.Errorf("%s is not under %q:\n%s", id, application.CausesMissingHeading, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportDoesNotListUnderCausesMissing(id string) error {
	section, err := c.causesMissingSection()
	if err != nil {
		return err
	}
	if strings.Contains(section, id) {
		return fmt.Errorf("%s is under %q, and should not be:\n%s", id, application.CausesMissingHeading, section)
	}
	return nil
}

func (c *statusContext) theReportHasNoCausesMissingHeading() error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if strings.Contains(c.report.String(), application.CausesMissingHeading) {
		return fmt.Errorf("expected no %q heading, got:\n%s", application.CausesMissingHeading, c.report.String())
	}
	return nil
}

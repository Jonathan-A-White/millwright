package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// registerEpicSteps registers the steps of features/status_epic_requirements.feature
// on the status context.
func (c *statusContext) registerEpicSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the status rig "([^"]*)" requires the epic sections "([^"]*)" and the last-story labels "([^"]*)"$`,
		c.theStatusRigRequires)
	ctx.Given(`^the status epic "([^"]*)" of the rig "([^"]*)" described as "([^"]*)"$`, c.theStatusEpicOfTheRig)
	ctx.Given(`^the status epic "([^"]*)" carries the waiver labels for "([^"]*)" and "([^"]*)"$`, c.theStatusEpicCarriesTheWaiverLabels)
	ctx.Given(`^the status epic "([^"]*)" is closed$`, c.theStatusEpicIsClosed)
	ctx.Given(`^the status epic "([^"]*)" is labelled "([^"]*)"$`, c.theStatusEpicIsLabelled)
	ctx.Then(`^the report lists "([^"]*)" under (EPICS MISSING REQUIREMENTS|EPICS WAIVED) saying "([^"]*)"$`,
		c.theReportListsTheEpicUnder)
	ctx.Then(`^the report does not list "([^"]*)" under (EPICS MISSING REQUIREMENTS|EPICS WAIVED)$`,
		c.theReportDoesNotListTheEpicUnder)
	ctx.Then(`^the report has no (EPICS MISSING REQUIREMENTS|EPICS WAIVED) section$`, c.theReportHasNoEpicSection)
}

func (c *statusContext) theStatusRigRequires(rig, sections, labels string) error {
	c.epicRules.Require(rig, domain.EpicRequirements{Sections: []string{sections}, LastStoryLabels: []string{labels}})
	return nil
}

func (c *statusContext) theStatusEpicOfTheRig(id, rig, description string) error {
	c.tracker.AddEpic(id, domain.Path{Rig: rig, Branch: "main", Harness: "claude", Model: "opus", Effort: "high", Host: statusHost})
	c.tracker.DescribeEpicText(id, strings.ReplaceAll(description, `\n`, "\n"))
	c.lastEpic = id
	return nil
}

func (c *statusContext) theStatusEpicCarriesTheWaiverLabels(id, first, second string) error {
	// The first names the section, the second the last-story label.
	for _, label := range []string{domain.WaiverLabel(true, first), domain.WaiverLabel(false, second)} {
		if err := c.tracker.AddLabel(context.Background(), id, label); err != nil {
			return err
		}
	}
	return nil
}

func (c *statusContext) theStatusEpicIsClosed(id string) error {
	c.tracker.DescribeEpic(id, id, application.StatusClosed, application.DefaultPriority)
	return nil
}

func (c *statusContext) theStatusEpicIsLabelled(id, label string) error {
	return c.tracker.AddLabel(context.Background(), id, label)
}

// sectionOf is the lines of the report under one heading, up to its blank line.
func (c *statusContext) sectionOf(heading string) ([]string, bool) {
	lines := strings.Split(c.report.String(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, heading+" (") {
			var section []string
			for _, next := range lines[i+1:] {
				if next == "" {
					break
				}
				section = append(section, next)
			}
			return section, true
		}
	}
	return nil, false
}

func (c *statusContext) theReportListsTheEpicUnder(id, heading, saying string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	section, found := c.sectionOf(heading)
	if !found {
		return fmt.Errorf("expected a %s section, got:\n%s", heading, c.report.String())
	}
	for _, line := range section {
		if strings.Contains(line, id) && strings.Contains(line, saying) {
			return nil
		}
	}
	return fmt.Errorf("expected %s under %s saying %q, got:\n%s", id, heading, saying, strings.Join(section, "\n"))
}

func (c *statusContext) theReportHasNoEpicSection(heading string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	if _, found := c.sectionOf(heading); found {
		return fmt.Errorf("expected no %s section, got:\n%s", heading, c.report.String())
	}
	return nil
}

func (c *statusContext) theReportDoesNotListTheEpicUnder(id, heading string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	section, _ := c.sectionOf(heading)
	for _, line := range section {
		if strings.Contains(line, id) {
			return fmt.Errorf("expected %s not under %s, got:\n%s", id, heading, strings.Join(section, "\n"))
		}
	}
	return nil
}

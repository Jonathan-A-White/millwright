package steps

import (
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerStatusVisionSteps registers the steps of features/status.feature that
// are about the VISION lines (mw-7vrp1r.1). They are called after the Before
// hook, so the context they fill is the scenario's own.
func registerStatusVisionSteps(ctx *godog.ScenarioContext, c *statusContext) {
	ctx.Given(`^the host's \[grist-apps\] checks out the app "([^"]*)" and the app "([^"]*)"$`, c.theHostChecksOutApps)
	ctx.Given(`^the checkout of the app "([^"]*)" has a VISION.md$`, c.theCheckoutHasAVision)
	ctx.Then(`^the report has the line "([^"]*)"$`, c.theReportHasTheLine)
	ctx.Then(`^the report has no line "([^"]*)"$`, c.theReportHasNoLine)
	ctx.Then(`^the report has no line starting "([^"]*)"$`, c.theReportHasNoLineStarting)
}

func (c *statusContext) theHostChecksOutApps(first, second string) error {
	c.apps = map[string]string{first: "/rigs/" + first, second: "/rigs/" + second}
	c.visions = apptest.NewFakeVisions()
	return nil
}

func (c *statusContext) theCheckoutHasAVision(app string) error {
	dir, ok := c.apps[app]
	if !ok {
		return fmt.Errorf("the app %q is not in the host's [grist-apps]", app)
	}
	c.visions.Set(dir, "What "+app+" is for.")
	return nil
}

func (c *statusContext) theReportHasTheLine(line string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, got := range strings.Split(c.report.String(), "\n") {
		if got == line {
			return nil
		}
	}
	return fmt.Errorf("expected the line %q in the report:\n%s", line, c.report.String())
}

func (c *statusContext) theReportHasNoLine(line string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, got := range strings.Split(c.report.String(), "\n") {
		if got == line {
			return fmt.Errorf("the report has the line %q, and should not:\n%s", line, c.report.String())
		}
	}
	return nil
}

func (c *statusContext) theReportHasNoLineStarting(prefix string) error {
	if err := c.readingStatusSucceeds(); err != nil {
		return err
	}
	for _, got := range strings.Split(c.report.String(), "\n") {
		if strings.HasPrefix(got, prefix) {
			return fmt.Errorf("the report has the line %q, and should not:\n%s", got, c.report.String())
		}
	}
	return nil
}

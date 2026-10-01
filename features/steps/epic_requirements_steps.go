package steps

import (
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// registerRequirementSteps registers the steps of features/epic_requirements.feature
// on the file context: what a rig requires of its epics, the plan filed
// against it, and the Governor's waiver.
func (c *fileContext) registerRequirementSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the rig "([^"]*)" requires the epic sections "([^"]*)" and the last-story labels "([^"]*)"$`, c.theRigRequires)
	ctx.Given(`^the requiring plan for the rig "([^"]*)" with a description of "([^"]*)" and these stories:$`, c.theRequiringPlan)
	ctx.When(`^the requiring plan is filed$`, c.thePlanIsFiled)
	ctx.When(`^the requiring plan is filed waiving "([^"]*)" because "([^"]*)"$`, c.filedWaivingOne)
	ctx.When(`^the requiring plan is filed waiving "([^"]*)" and "([^"]*)" because "([^"]*)"$`, c.filedWaivingTwo)
	ctx.When(`^the requiring plan is filed waiving "([^"]*)" and "([^"]*)" with no words$`, c.filedWaivingWithNoWords)
	ctx.Then(`^the filed story "([^"]*)" carries the label "([^"]*)"$`, c.theFiledStoryCarriesTheLabel)
	ctx.Then(`^the filed epic carries the waiver labels for "([^"]*)" and "([^"]*)"$`, c.theFiledEpicCarriesTheWaiverLabels)
	ctx.Then(`^the filed epic has a comment quoting "([^"]*)"$`, c.theFiledEpicHasACommentQuoting)
}

func (c *fileContext) theRigRequires(rig, sections, labels string) error {
	c.rules.Require(rig, domain.EpicRequirements{Sections: []string{sections}, LastStoryLabels: []string{labels}})
	return nil
}

// theRequiringPlan builds a plan for the rig out of a table of stories.
func (c *fileContext) theRequiringPlan(rig, description string, table *godog.Table) error {
	plan := domain.Plan{Epic: domain.PlanEpic{
		Key:         "epic",
		Title:       "An epic",
		Description: strings.ReplaceAll(description, `\n`, "\n"),
		Defaults: domain.Path{Rig: rig, Branch: "main", Harness: "claude", Model: "opus", Effort: "high",
			Host: "vps"},
	}}
	columns := map[string]int{}
	for i, cell := range table.Rows[0].Cells {
		columns[cell.Value] = i
	}
	for _, row := range table.Rows[1:] {
		story := domain.PlanStory{
			Key:        row.Cells[columns["key"]].Value,
			Title:      row.Cells[columns["key"]].Value,
			Acceptance: "it works",
			Labels:     splitList(row.Cells[columns["labels"]].Value),
			Needs:      splitList(row.Cells[columns["needs"]].Value),
		}
		plan.Stories = append(plan.Stories, story)
	}
	c.plan = plan
	return nil
}

func splitList(text string) []string {
	var items []string
	for _, item := range strings.Split(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func (c *fileContext) filedWaivingOne(name, because string) error {
	c.waive = application.EpicWaiver{Names: []string{name}, Because: because}
	return c.file(nil)
}

func (c *fileContext) filedWaivingTwo(first, second, because string) error {
	c.waive = application.EpicWaiver{Names: []string{first, second}, Because: because}
	return c.file(nil)
}

func (c *fileContext) filedWaivingWithNoWords(first, second string) error {
	c.waive = application.EpicWaiver{Names: []string{first, second}}
	return c.file(nil)
}

func (c *fileContext) theFiledStoryCarriesTheLabel(key, label string) error {
	detail, err := c.detailOf(key)
	if err != nil {
		return err
	}
	for _, have := range detail.Labels {
		if have == label {
			return nil
		}
	}
	return fmt.Errorf("expected the filed story %s to carry the label %q, it carries %v", key, label, detail.Labels)
}

func (c *fileContext) theFiledEpicCarriesTheWaiverLabels(first, second string) error {
	if err := c.filingSucceeds(); err != nil {
		return err
	}
	labels := c.tracker.EpicLabels(c.filed.EpicID)
	// The first names the section, the second the last-story label.
	for _, lack := range []domain.Shortfall{{Name: first, Section: true}, {Name: second}} {
		if !domain.Waives(labels, lack) {
			return fmt.Errorf("expected the epic %s to carry the waiver label for %s, it carries %v", c.filed.EpicID, lack.Name, labels)
		}
	}
	return nil
}

func (c *fileContext) theFiledEpicHasACommentQuoting(words string) error {
	if err := c.filingSucceeds(); err != nil {
		return err
	}
	for _, comment := range c.tracker.EpicComments(c.filed.EpicID) {
		if strings.Contains(comment, `"`+words+`"`) {
			return nil
		}
	}
	return fmt.Errorf("expected a comment on %s quoting %q, got %v", c.filed.EpicID, words, c.tracker.EpicComments(c.filed.EpicID))
}

package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// testerFileContext is a landed story, its Tester story and a fake tracker to
// file the Tester's findings in: the steps of features/tester.feature that run
// mw tester file without a rig (mw-gq6.447).
type testerFileContext struct {
	tracker *apptest.FakeTracker
	tester  string
	filed   application.TesterFiled
	err     error
}

// registerTesterFileSteps registers the mw tester file steps of
// features/tester.feature. Its tracker is made by the Given, not a hook.
func registerTesterFileSteps(ctx *godog.ScenarioContext) {
	c := &testerFileContext{}
	ctx.Given(`^the landed story "([^"]*)" on "([^"]*)" and its Tester story "([^"]*)"$`, c.aLandedStoryAndItsTester)
	ctx.When(`^the Tester files the bug finding "([^"]*)" accepting:$`, c.theTesterFilesABugFinding)
	ctx.Then(`^the finding is refused, saying: (.+)$`, c.theFindingIsRefusedSaying)
	ctx.Then(`^no story was filed from the finding$`, c.noStoryWasFiledFromTheFinding)
	ctx.Then(`^the finding is filed held, labelled bug, its acceptance carrying "([^"]*)"$`, c.theFindingIsFiledHeld)
}

func (c *testerFileContext) aLandedStoryAndItsTester(landed, rig, tester string) error {
	epic, _, _ := strings.Cut(landed, ".")
	*c = testerFileContext{tracker: apptest.NewFakeTracker(), tester: tester}
	c.tracker.AddEpic(epic, domain.Path{Rig: rig, Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelSonnet,
		Effort: domain.EffortHigh, Host: "laptop", Formula: "tdd-feature"})
	c.tracker.AddStory(epic, domain.Story{ID: landed, Title: "Reader remembers its place"})
	c.tracker.AddStory(epic, domain.Story{ID: tester, Title: "Test: Reader remembers its place", Overrides: domain.Path{Formula: "tester"}})
	if err := c.tracker.SetDescription(tester, application.TesterLandedLine+landed+" (landed on main)\n\nDrive it."); err != nil {
		return err
	}
	return c.tracker.SetLabels(tester, application.LabelTester)
}

func (c *testerFileContext) theTesterFilesABugFinding(title string, acceptance *godog.DocString) error {
	if c.tracker == nil {
		return fmt.Errorf("no landed story was set up")
	}
	c.filed, c.err = application.TesterFiler{Tracker: c.tracker}.File(context.Background(), c.tester, application.TesterFinding{
		Kind:       application.TesterKindBug,
		Title:      title,
		Finding:    "Rotate and back: the Reader is on page 1 again.",
		HowToCheck: "1. Open page 7.\n2. Rotate the phone and back: page 7 is still shown.",
		Acceptance: acceptance.Content,
		Files:      "src/Reader.tsx",
	})
	return nil
}

func (c *testerFileContext) theFindingIsRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected the finding to be refused, saying %q; it was filed as %q", reason, c.filed.ID)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *testerFileContext) noStoryWasFiledFromTheFinding() error {
	for _, id := range c.tracker.Stories() {
		story, err := c.tracker.ShowStory(context.Background(), id)
		if err != nil {
			return err
		}
		if strings.HasPrefix(story.Story.Title, "[bug]") {
			return fmt.Errorf("expected nothing filed, got %s %q", id, story.Story.Title)
		}
	}
	return nil
}

func (c *testerFileContext) theFindingIsFiledHeld(want string) error {
	if c.err != nil {
		return fmt.Errorf("expected the finding filed, got: %v", c.err)
	}
	story, err := c.tracker.ShowStory(context.Background(), c.filed.ID)
	if err != nil {
		return err
	}
	if story.Status != apptest.StatusDeferred {
		return fmt.Errorf("expected %s held, got %q", c.filed.ID, story.Status)
	}
	if !strings.Contains(strings.Join(story.Labels, ","), application.LabelBug) {
		return fmt.Errorf("expected %s labelled bug, got %q", c.filed.ID, story.Labels)
	}
	if !strings.Contains(story.Acceptance, want) {
		return fmt.Errorf("expected the acceptance of %s to carry %q, got %q", c.filed.ID, want, story.Acceptance)
	}
	if !domain.SweepOf(story.Acceptance).Passes() {
		return fmt.Errorf("expected the acceptance of %s to pass the sweep rule, got %q", c.filed.ID, story.Acceptance)
	}
	return nil
}

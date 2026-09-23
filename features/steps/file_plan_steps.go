package steps

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// fileContext holds the plan being filed, the fake work tracker it is filed
// into, what came of filing it and what was printed while it was.
type fileContext struct {
	tracker *apptest.FakeTracker
	plan    domain.Plan
	read    error

	printed bytes.Buffer
	filed   application.FiledPlan
	err     error
}

// InitializeFilePlanScenario registers the steps of features/file_plan.feature.
func InitializeFilePlanScenario(ctx *godog.ScenarioContext) {
	c := &fileContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tracker := apptest.NewFakeTracker()
		// The Background's plan names both of these; a scenario that names a
		// formula neither of them is testing the refusal itself.
		tracker.AddFormula("tdd-feature")
		tracker.AddFormula("chore")
		*c = fileContext{tracker: tracker}
		return ctx, nil
	})

	ctx.Given(`^the plan:$`, c.thePlan)
	ctx.When(`^the plan is filed$`, c.thePlanIsFiled)
	ctx.When(`^the plan is filed and approved$`, c.thePlanIsFiledAndApproved)
	ctx.When(`^the story "([^"]*)" is closed$`, c.theStoryIsDone)
	ctx.Then(`^filing succeeds$`, c.filingSucceeds)
	ctx.Then(`^filing is refused, saying: (.+)$`, c.filingIsRefusedSaying)
	ctx.Then(`^nothing is written$`, c.nothingIsWritten)
	ctx.Then(`^the stories are filed in the order (.+)$`, c.theStoriesAreFiledInTheOrder)
	ctx.Then(`^the filed story "([^"]*)" carries its acceptance criteria$`, c.theFiledStoryCarriesItsAcceptanceCriteria)
	ctx.Then(`^the filed story "([^"]*)" carries an estimate of (\d+) minutes$`, c.theFiledStoryCarriesAnEstimateOf)
	ctx.Then(`^the filed story "([^"]*)" is worked on model (\S+) by formula (\S+)$`, c.theFiledStoryIsWorkedOnModelByFormula)
	ctx.Then(`^the tree names the epic (.+)$`, c.theTreeNamesTheEpic)
	ctx.Then(`^the tree says: (.+)$`, c.theTreeSays)
	ctx.Then(`^the tree shows the story "([^"]*)" on the path (.+)$`, c.theTreeShowsTheStoryOnThePath)
	ctx.Then(`^the tree shows the story "([^"]*)" waiting on "([^"]*)"$`, c.theTreeShowsTheStoryWaitingOn)
	ctx.Then(`^the tree shows the story "([^"]*)" waiting on nothing$`, c.theTreeShowsTheStoryWaitingOnNothing)
	ctx.Then(`^the tree shows the story "([^"]*)" held$`, c.theTreeShowsTheStoryHeld)
	ctx.Then(`^no story of the epic is ready on (\S+)$`, c.noStoryOfTheEpicIsReadyOn)
	ctx.Then(`^the ready stories of the epic on (\S+) are (.+)$`, c.theReadyStoriesOfTheEpicOn)
}

func (c *fileContext) thePlan(written *godog.DocString) error {
	c.plan, c.read = domain.ParsePlan([]byte(written.Content))
	return nil
}

// file files the plan the scenario gave, with whoever decides whether to
// release it — nobody, when approve is nil.
func (c *fileContext) file(approve func(context.Context, application.FiledPlan) (bool, error)) error {
	if c.read != nil {
		return fmt.Errorf("the plan could not be read: %w", c.read)
	}
	c.filed, c.err = application.File{
		Tracker: c.tracker,
		Out:     &c.printed,
		Approve: approve,
	}.Run(context.Background(), c.plan)
	return nil
}

func (c *fileContext) thePlanIsFiled() error {
	return c.file(nil)
}

func (c *fileContext) thePlanIsFiledAndApproved() error {
	return c.file(func(context.Context, application.FiledPlan) (bool, error) { return true, nil })
}

func (c *fileContext) theStoryIsDone(key string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	return c.tracker.CloseStory(context.Background(), id, "worked")
}

func (c *fileContext) filingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("filing the plan failed: %w", c.err)
	}
	return nil
}

func (c *fileContext) filingIsRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected filing to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *fileContext) nothingIsWritten() error {
	if epics := c.tracker.Epics(); len(epics) != 0 {
		return fmt.Errorf("expected no epic to be filed, got %s", strings.Join(epics, ", "))
	}
	if stories := c.tracker.Stories(); len(stories) != 0 {
		return fmt.Errorf("expected no story to be filed, got %s", strings.Join(stories, ", "))
	}
	return nil
}

func (c *fileContext) theStoriesAreFiledInTheOrder(want string) error {
	if err := c.filingSucceeds(); err != nil {
		return err
	}
	keys := make([]string, 0, len(c.filed.Stories))
	for _, story := range c.filed.Stories {
		keys = append(keys, story.Key)
	}
	if got := strings.Join(keys, ", "); got != want {
		return fmt.Errorf("expected the stories to be filed in the order %q, got %q", want, got)
	}
	// The tracker holds exactly those stories, and no more.
	if filed := len(c.tracker.Stories()); filed != len(c.filed.Stories) {
		return fmt.Errorf("expected %d stories in the tracker, got %d", len(c.filed.Stories), filed)
	}
	return nil
}

func (c *fileContext) theFiledStoryCarriesItsAcceptanceCriteria(key string) error {
	detail, err := c.detailOf(key)
	if err != nil {
		return err
	}
	if strings.TrimSpace(detail.Acceptance) == "" {
		return fmt.Errorf("expected the story %s to carry its acceptance criteria, it carries none", key)
	}
	return nil
}

func (c *fileContext) theFiledStoryCarriesAnEstimateOf(key string, minutes int) error {
	detail, err := c.detailOf(key)
	if err != nil {
		return err
	}
	if detail.EstimateMinutes != minutes {
		return fmt.Errorf("expected the story %s to be estimated at %d minutes, got %d", key, minutes, detail.EstimateMinutes)
	}
	return nil
}

func (c *fileContext) theFiledStoryIsWorkedOnModelByFormula(key, model, formula string) error {
	detail, err := c.detailOf(key)
	if err != nil {
		return err
	}
	path, err := detail.Path()
	if err != nil {
		return fmt.Errorf("the filed story %s has no path: %w", key, err)
	}
	if string(path.Model) != model || path.Formula != formula {
		return fmt.Errorf("expected the story %s to be worked on model %q by formula %q, got %q and %q",
			key, model, formula, path.Model, path.Formula)
	}
	return nil
}

func (c *fileContext) theTreeNamesTheEpic(title string) error {
	if err := c.filingSucceeds(); err != nil {
		return err
	}
	line := c.filed.EpicID + " · " + title
	if !strings.Contains(c.printed.String(), line) {
		return fmt.Errorf("expected the tree to name the epic as %q, got:\n%s", line, c.printed.String())
	}
	return nil
}

func (c *fileContext) theTreeSays(said string) error {
	if !strings.Contains(c.printed.String(), said) {
		return fmt.Errorf("expected the tree to say %q, got:\n%s", said, c.printed.String())
	}
	return nil
}

func (c *fileContext) theTreeShowsTheStoryOnThePath(key, path string) error {
	return c.storyBlockSays(key, "path "+path)
}

func (c *fileContext) theTreeShowsTheStoryWaitingOn(key, need string) error {
	id, err := c.idOf(need)
	if err != nil {
		return err
	}
	return c.storyBlockSays(key, "waits on "+id)
}

func (c *fileContext) theTreeShowsTheStoryWaitingOnNothing(key string) error {
	return c.storyBlockSays(key, "waits on nothing")
}

func (c *fileContext) theTreeShowsTheStoryHeld(key string) error {
	return c.storyBlockSays(key, "held")
}

func (c *fileContext) noStoryOfTheEpicIsReadyOn(host string) error {
	ready, err := c.readyOn(host)
	if err != nil {
		return err
	}
	if len(ready) != 0 {
		return fmt.Errorf("expected nothing to be ready on %s, got %s", host, strings.Join(apptest.IDs(ready), ", "))
	}
	return nil
}

func (c *fileContext) theReadyStoriesOfTheEpicOn(host, want string) error {
	ready, err := c.readyOn(host)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(ready))
	for _, detail := range ready {
		key, err := c.keyOf(detail.Story.ID)
		if err != nil {
			return err
		}
		keys = append(keys, key)
	}
	if got := strings.Join(keys, ", "); got != want {
		return fmt.Errorf("expected the ready stories on %s to be %q, got %q", host, want, got)
	}
	return nil
}

// readyOn lists the stories of the filed epic a dispatcher could take on a host.
func (c *fileContext) readyOn(host string) ([]application.StoryDetail, error) {
	if err := c.filingSucceeds(); err != nil {
		return nil, err
	}
	ready, err := c.tracker.ReadyStories(context.Background(), c.filed.EpicID, host)
	if err != nil {
		return nil, fmt.Errorf("listing the ready stories of %s: %w", c.filed.EpicID, err)
	}
	return ready, nil
}

// storyBlockSays checks that what the tree prints about one story says this.
// The tree gives each story three lines, so the block is the story's own line
// and the two under it.
func (c *fileContext) storyBlockSays(key, said string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	lines := strings.Split(c.printed.String(), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), id+" · ") {
			continue
		}
		block := strings.Join(lines[i:min(i+3, len(lines))], "\n")
		if !strings.Contains(block, said) {
			return fmt.Errorf("expected the tree to say %q about %s (%s), got:\n%s", said, key, id, block)
		}
		return nil
	}
	return fmt.Errorf("the tree does not show the story %s (%s):\n%s", key, id, c.printed.String())
}

// detailOf reads back what the tracker holds about one filed story.
func (c *fileContext) detailOf(key string) (application.StoryDetail, error) {
	id, err := c.idOf(key)
	if err != nil {
		return application.StoryDetail{}, err
	}
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return application.StoryDetail{}, fmt.Errorf("reading back the filed story %s (%s): %w", key, id, err)
	}
	return detail, nil
}

// idOf is the id the tracker gave the story the plan calls key.
func (c *fileContext) idOf(key string) (string, error) {
	if err := c.filingSucceeds(); err != nil {
		return "", err
	}
	for _, story := range c.filed.Stories {
		if story.Key == key {
			return story.ID, nil
		}
	}
	return "", fmt.Errorf("no story %q was filed", key)
}

// keyOf is what the plan called the story with this id.
func (c *fileContext) keyOf(id string) (string, error) {
	for _, story := range c.filed.Stories {
		if story.ID == id {
			return story.Key, nil
		}
	}
	return "", fmt.Errorf("no story with the id %q was filed", id)
}

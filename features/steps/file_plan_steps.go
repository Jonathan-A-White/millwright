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
	rules   *apptest.FakeEpicRules
	waive   application.EpicWaiver
	plan    domain.Plan
	read    error

	// seeded are the ids of the stories a scenario put in the tracker before the
	// plan was filed.
	seeded []string

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
		*c = fileContext{tracker: tracker, rules: apptest.NewFakeEpicRules()}
		return ctx, nil
	})

	c.registerRequirementSteps(ctx)

	ctx.Given(`^the plan:$`, c.thePlan)
	ctx.Given(`^the story "([^"]*)" has a title of (\d+) characters$`, c.theStoryHasATitleOf)
	ctx.Given(`^the epic has a title of (\d+) characters$`, c.theEpicHasATitleOf)
	ctx.Given(`^the story "([^"]*)" names the files "([^"]*)"$`, c.theStoryNamesTheFiles)
	ctx.Given(`^the story "([^"]*)" "([^"]*)" is (running|open) on the rig "([^"]*)" with the files "([^"]*)"$`, c.aStoryIsInFlight)
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
	ctx.Then(`^the filed story "([^"]*)" carries the files key "([^"]*)"$`, c.theFiledStoryCarriesTheFilesKey)
	ctx.Then(`^the tree shows under the story "([^"]*)": (.+)$`, c.theTreeShowsUnderTheStory)
	ctx.Then(`^the tree shows under the story "([^"]*)" the dependency to add on "([^"]*)"$`, c.theTreeShowsTheDependencyToAdd)
	ctx.Then(`^the tree shows nothing in flight under the story "([^"]*)"$`, c.theTreeShowsNothingInFlight)
	ctx.Then(`^the tree does not name "([^"]*)"$`, c.theTreeDoesNotName)
	ctx.Then(`^no dependency was added$`, c.noDependencyWasAdded)
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

func (c *fileContext) theStoryHasATitleOf(key string, length int) error {
	for i := range c.plan.Stories {
		if c.plan.Stories[i].Key == key {
			c.plan.Stories[i].Title = strings.Repeat("t", length)
			return nil
		}
	}
	return fmt.Errorf("the plan has no story %q", key)
}

func (c *fileContext) theStoryNamesTheFiles(key, files string) error {
	for i := range c.plan.Stories {
		if c.plan.Stories[i].Key == key {
			c.plan.Stories[i].Files = files
			return nil
		}
	}
	return fmt.Errorf("the plan has no story %q", key)
}

// aStoryIsInFlight puts a story the plan did not file in the tracker, open or
// running, as the Mayor's earlier work would have left it.
func (c *fileContext) aStoryIsInFlight(id, title, status, rig, files string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: title, Overrides: domain.Path{Rig: rig, Files: files}})
	c.seeded = append(c.seeded, id)
	if status == "running" {
		return c.tracker.SetStatus(id, apptest.StatusInProgress)
	}
	return nil
}

func (c *fileContext) theFiledStoryCarriesTheFilesKey(key, files string) error {
	detail, err := c.detailOf(key)
	if err != nil {
		return err
	}
	if got := detail.Merged().Files; got != files {
		return fmt.Errorf("expected the story %s to carry the files key %q, got %q", key, files, got)
	}
	return nil
}

// treeLinesUnder are the lines the tree prints under a story's own: all of them
// up to the next story, or the end of the tree.
func (c *fileContext) treeLinesUnder(key string) ([]string, error) {
	id, err := c.idOf(key)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(c.printed.String(), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), id+" · ") {
			continue
		}
		var under []string
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(next, "  ") && !strings.HasPrefix(next, "   ") || strings.TrimSpace(next) == "" {
				break
			}
			under = append(under, strings.TrimSpace(next))
		}
		return under, nil
	}
	return nil, fmt.Errorf("the tree does not show the story %s (%s):\n%s", key, id, c.printed.String())
}

func (c *fileContext) theTreeShowsUnderTheStory(key, said string) error {
	under, err := c.treeLinesUnder(key)
	if err != nil {
		return err
	}
	for _, line := range under {
		if line == said {
			return nil
		}
	}
	return fmt.Errorf("expected the tree to say %q under %s, got:\n%s", said, key, strings.Join(under, "\n"))
}

func (c *fileContext) theTreeShowsTheDependencyToAdd(key, flight string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	return c.theTreeShowsUnderTheStory(key, "bd dep add "+id+" "+flight)
}

func (c *fileContext) theTreeShowsNothingInFlight(key string) error {
	under, err := c.treeLinesUnder(key)
	if err != nil {
		return err
	}
	for _, line := range under {
		if strings.HasPrefix(line, "in flight:") || strings.HasPrefix(line, "bd dep add") {
			return fmt.Errorf("expected nothing in flight under %s, got:\n%s", key, strings.Join(under, "\n"))
		}
	}
	return nil
}

func (c *fileContext) theTreeDoesNotName(id string) error {
	if strings.Contains(c.printed.String(), id) {
		return fmt.Errorf("expected the tree not to name %s, got:\n%s", id, c.printed.String())
	}
	return nil
}

// noDependencyWasAdded checks that nothing the plan filed waits on a story it
// found in flight: the tree only suggests the dependency.
func (c *fileContext) noDependencyWasAdded() error {
	if err := c.filingSucceeds(); err != nil {
		return err
	}
	for _, story := range c.filed.Stories {
		detail, err := c.tracker.ShowStory(context.Background(), story.ID)
		if err != nil {
			return err
		}
		for _, need := range detail.Needs {
			for _, seeded := range c.seeded {
				if need == seeded {
					return fmt.Errorf("expected nothing to be made to wait on %s, but %s does", seeded, story.ID)
				}
			}
		}
	}
	return nil
}

func (c *fileContext) theEpicHasATitleOf(length int) error {
	c.plan.Epic.Title = strings.Repeat("t", length)
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
		Rules:   c.rules,
		Waive:   c.waive,
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

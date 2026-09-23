package steps

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// releaseContext holds a plan filed and held earlier, the fake work tracker it
// was filed into, and what came of releasing it.
type releaseContext struct {
	tracker *apptest.FakeTracker
	filed   application.FiledPlan

	printed bytes.Buffer
	err     error

	// writesBefore is how many writes the tracker had taken when the epic was
	// last shown.
	writesBefore int
}

// InitializeReleaseScenario registers the steps of features/release.feature.
func InitializeReleaseScenario(ctx *godog.ScenarioContext) {
	c := &releaseContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tracker := apptest.NewFakeTracker()
		// The plans this feature and show.feature file both name these formulas.
		tracker.AddFormula("tdd-feature")
		tracker.AddFormula("chore")
		*c = releaseContext{tracker: tracker}
		return ctx, nil
	})

	ctx.Given(`^the plan, filed and held earlier:$`, c.thePlanFiledAndHeldEarlier)
	ctx.Given(`^the story "([^"]*)" of the filed plan is finished$`, c.theStoryOfTheFiledPlanIsFinished)
	ctx.Given(`^the story "([^"]*)" of the filed plan is taken by a session$`, c.theStoryOfTheFiledPlanIsTaken)
	ctx.When(`^the epic is released$`, c.theEpicIsReleased)
	ctx.When(`^the epic is released again$`, c.theEpicIsReleased)
	ctx.When(`^the epic "([^"]*)" is released$`, c.theNamedEpicIsReleased)
	ctx.Then(`^releasing succeeds$`, c.releasingSucceeds)
	ctx.Then(`^releasing is refused, saying: (.+)$`, c.releasingIsRefusedSaying)
	ctx.Then(`^the release tree names the epic (.+)$`, c.theReleaseTreeNamesTheEpic)
	ctx.Then(`^the release tree shows the story "([^"]*)" on the path (.+)$`, c.theReleaseTreeShowsTheStoryOnThePath)
	ctx.Then(`^the release tree shows the story "([^"]*)" waiting on "([^"]*)"$`, c.theReleaseTreeShowsTheStoryWaitingOn)
	ctx.Then(`^the release tree shows the story "([^"]*)" as (.+)$`, c.theReleaseTreeShowsTheStoryAs)
	ctx.Then(`^the release says: (.+)$`, c.theReleaseSays)
	ctx.Then(`^the story "([^"]*)" of the filed plan is now (.+)$`, c.theStoryOfTheFiledPlanIsNow)
	ctx.Then(`^the stories ready on (\S+) once released are (.+)$`, c.theStoriesReadyOnceReleasedAre)
	ctx.Then(`^every story of the filed plan is still held$`, c.everyStoryOfTheFiledPlanIsStillHeld)

	c.registerShowSteps(ctx)
}

// thePlanFiledAndHeldEarlier files the plan the way `mw file` does when nobody
// is there to approve it: every story in the tracker, every story held.
func (c *releaseContext) thePlanFiledAndHeldEarlier(written *godog.DocString) error {
	plan, err := domain.ParsePlan([]byte(written.Content))
	if err != nil {
		return fmt.Errorf("the plan could not be read: %w", err)
	}
	c.filed, err = application.File{Tracker: c.tracker, Out: io.Discard}.Run(context.Background(), plan)
	if err != nil {
		return fmt.Errorf("filing the plan: %w", err)
	}
	if c.filed.Released {
		return fmt.Errorf("expected the filed plan to be held, it was released")
	}
	return nil
}

func (c *releaseContext) theStoryOfTheFiledPlanIsFinished(key string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	return c.tracker.CloseStory(context.Background(), id, "worked")
}

func (c *releaseContext) theStoryOfTheFiledPlanIsTaken(key string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	return c.tracker.ClaimStory(context.Background(), id)
}

func (c *releaseContext) theEpicIsReleased() error {
	return c.release(c.filed.EpicID)
}

func (c *releaseContext) theNamedEpicIsReleased(epicID string) error {
	return c.release(epicID)
}

// release runs the use case, keeping only what this run printed: a second
// release is read on its own, not on top of the first.
func (c *releaseContext) release(epicID string) error {
	c.printed.Reset()
	_, c.err = application.Release{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), epicID)
	return nil
}

func (c *releaseContext) releasingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("releasing the epic failed: %w", c.err)
	}
	return nil
}

func (c *releaseContext) releasingIsRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected releasing to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *releaseContext) theReleaseTreeNamesTheEpic(title string) error {
	if err := c.releasingSucceeds(); err != nil {
		return err
	}
	line := c.filed.EpicID + " · " + title
	if !strings.Contains(c.printed.String(), line) {
		return fmt.Errorf("expected the tree to name the epic as %q, got:\n%s", line, c.printed.String())
	}
	return nil
}

func (c *releaseContext) theReleaseTreeShowsTheStoryOnThePath(key, path string) error {
	return c.treeBlockSays(key, "path "+path)
}

func (c *releaseContext) theReleaseTreeShowsTheStoryWaitingOn(key, need string) error {
	id, err := c.idOf(need)
	if err != nil {
		return err
	}
	return c.treeBlockSays(key, "waits on "+id)
}

func (c *releaseContext) theReleaseTreeShowsTheStoryAs(key, state string) error {
	return c.treeBlockSays(key, state)
}

func (c *releaseContext) theReleaseSays(said string) error {
	if !strings.Contains(c.printed.String(), said) {
		return fmt.Errorf("expected the release to say %q, got:\n%s", said, c.printed.String())
	}
	return nil
}

func (c *releaseContext) theStoryOfTheFiledPlanIsNow(key, state string) error {
	id, err := c.idOf(key)
	if err != nil {
		return err
	}
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return fmt.Errorf("reading back the story %s (%s): %w", key, id, err)
	}
	if got := application.StateOf(detail.Status); got != state {
		return fmt.Errorf("expected the story %s (%s) to be %s now, it is %s", key, id, state, got)
	}
	return nil
}

func (c *releaseContext) theStoriesReadyOnceReleasedAre(host, want string) error {
	if err := c.releasingSucceeds(); err != nil {
		return err
	}
	ready, err := c.tracker.ReadyStories(context.Background(), c.filed.EpicID, host)
	if err != nil {
		return fmt.Errorf("listing the ready stories of %s: %w", c.filed.EpicID, err)
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

func (c *releaseContext) everyStoryOfTheFiledPlanIsStillHeld() error {
	for _, story := range c.filed.Stories {
		detail, err := c.tracker.ShowStory(context.Background(), story.ID)
		if err != nil {
			return fmt.Errorf("reading back the story %s: %w", story.ID, err)
		}
		if !detail.Held() {
			return fmt.Errorf("expected the story %s to be held still, it is %s", story.ID, detail.Status)
		}
	}
	return nil
}

// treeBlockSays checks that what the tree prints about one story says this. The
// tree gives each story three lines, so the block is the story's own line and
// the two under it.
func (c *releaseContext) treeBlockSays(key, said string) error {
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

// idOf is the id the tracker gave the story the plan calls key.
func (c *releaseContext) idOf(key string) (string, error) {
	for _, story := range c.filed.Stories {
		if story.Key == key {
			return story.ID, nil
		}
	}
	return "", fmt.Errorf("no story %q was filed", key)
}

// keyOf is what the plan called the story with this id.
func (c *releaseContext) keyOf(id string) (string, error) {
	for _, story := range c.filed.Stories {
		if story.ID == id {
			return story.Key, nil
		}
	}
	return "", fmt.Errorf("no story with the id %q was filed", id)
}

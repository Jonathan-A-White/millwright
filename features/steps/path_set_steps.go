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

// pathSetContext holds the fake work tracker, the stories the scenario made
// and what came of setting or showing a path.
type pathSetContext struct {
	tracker *apptest.FakeTracker

	printed bytes.Buffer
	err     error

	// writesBefore is how many writes the tracker had taken when the command ran.
	writesBefore int
}

// InitializePathSetScenario registers the steps of features/path_set.feature.
func InitializePathSetScenario(ctx *godog.ScenarioContext) {
	c := &pathSetContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = pathSetContext{tracker: apptest.NewFakeTracker()}
		c.tracker.AddFormula("tdd-feature")
		return ctx, nil
	})

	ctx.Given(`^the path-set story "([^"]*)" is open with the metadata:$`, c.aStoryIsOpenWithTheMetadata)
	ctx.Given(`^the path-set story "([^"]*)" is open with no metadata$`, c.aStoryIsOpenWithNoMetadata)
	ctx.Given(`^the path-set story "([^"]*)" is in progress$`, c.aStoryIsInProgress)
	ctx.Given(`^the path-set story "([^"]*)" is closed$`, c.aStoryIsClosed)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)"$`, c.thePathIsSetFrom)
	ctx.When(`^the path of "([^"]*)" is set from "([^"]*)" with (.+)$`, c.thePathIsSetFromWith)
	ctx.When(`^the path of "([^"]*)" is set with no keys$`, c.thePathIsSetWithNoKeys)
	ctx.When(`^the path of "([^"]*)" is set with (\S.*=.*)$`, c.thePathIsSetWith)
	ctx.When(`^the path of "([^"]*)" is shown$`, c.thePathIsShown)
	ctx.Then(`^setting the path succeeds$`, c.settingSucceeds)
	ctx.Then(`^setting the path is refused, saying: (.+)$`, c.settingIsRefusedSaying)
	ctx.Then(`^nothing was written$`, c.nothingWasWritten)
	ctx.Then(`^the path-set took exactly (\d+) write$`, c.tookWrites)
	ctx.Then(`^the story "([^"]*)" has exactly the metadata:$`, c.theStoryHasExactlyTheMetadata)
	ctx.Then(`^the story "([^"]*)" has the path key "([^"]*)" as "([^"]*)"$`, c.theStoryHasThePathKey)
	ctx.Then(`^the path-set printed the line: (.+)$`, c.printedTheLine)
	ctx.Then(`^the path shown has the line: (.+)$`, c.printedTheLine)
	ctx.Then(`^the path-set echoed what showing the path of "([^"]*)" prints$`, c.echoedWhatShowPrints)
}

func (c *pathSetContext) aStoryIsOpenWithNoMetadata(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return nil
}

func (c *pathSetContext) aStoryIsOpenWithTheMetadata(id string, table *godog.Table) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	fields := map[string]string{}
	for _, row := range table.Rows {
		if len(row.Cells) != 2 {
			return fmt.Errorf("metadata rows need a key and a value, got %d cells", len(row.Cells))
		}
		fields[row.Cells[0].Value] = row.Cells[1].Value
	}
	return c.tracker.SetStoryMetadata(context.Background(), id, fields)
}

func (c *pathSetContext) aStoryIsInProgress(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return c.tracker.SetStatus(id, apptest.StatusInProgress)
}

func (c *pathSetContext) aStoryIsClosed(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "story " + id})
	return c.tracker.CloseStory(context.Background(), id, "done")
}

func (c *pathSetContext) set(id, from string, pairs []string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	_, c.err = application.PathSet{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id, from, pairs)
	return nil
}

func (c *pathSetContext) thePathIsSetFrom(id, from string) error {
	return c.set(id, from, nil)
}

func (c *pathSetContext) thePathIsSetFromWith(id, from, pairs string) error {
	return c.set(id, from, strings.Fields(pairs))
}

func (c *pathSetContext) thePathIsSetWith(id, pairs string) error {
	return c.set(id, "", strings.Fields(pairs))
}

func (c *pathSetContext) thePathIsSetWithNoKeys(id string) error {
	return c.set(id, "", nil)
}

func (c *pathSetContext) thePathIsShown(id string) error {
	c.printed.Reset()
	_, c.err = application.PathShow{Tracker: c.tracker, Out: &c.printed}.Run(context.Background(), id)
	return c.err
}

func (c *pathSetContext) settingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("setting the path failed: %w", c.err)
	}
	return nil
}

func (c *pathSetContext) settingIsRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected setting the path to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *pathSetContext) nothingWasWritten() error {
	return c.tookWrites(0)
}

func (c *pathSetContext) tookWrites(want int) error {
	if got := c.tracker.Writes() - c.writesBefore; got != want {
		return fmt.Errorf("expected %d writes, the tracker took %d", want, got)
	}
	return nil
}

func (c *pathSetContext) theStoryHasExactlyTheMetadata(id string, table *godog.Table) error {
	want := map[string]string{}
	for _, row := range table.Rows {
		want[row.Cells[0].Value] = row.Cells[1].Value
	}
	got := c.tracker.Metadata(id)
	if len(got) != len(want) {
		return fmt.Errorf("expected the story %s to hold exactly %v, it holds %v", id, want, got)
	}
	for key, value := range want {
		if got[key] != value {
			return fmt.Errorf("expected the story %s to hold exactly %v, it holds %v", id, want, got)
		}
	}
	return nil
}

func (c *pathSetContext) theStoryHasThePathKey(id, key, want string) error {
	if got := c.tracker.Metadata(id)[key]; got != want {
		return fmt.Errorf("expected the story %s to have %s=%s, it has %q", id, key, want, got)
	}
	return nil
}

func (c *pathSetContext) printedTheLine(line string) error {
	for _, got := range strings.Split(c.printed.String(), "\n") {
		if got == line {
			return nil
		}
	}
	return fmt.Errorf("expected the line %q, got:\n%s", line, c.printed.String())
}

// echoedWhatShowPrints holds the echo of the last set to the keys that `mw path
// show` prints for the story now: the same nine lines.
func (c *pathSetContext) echoedWhatShowPrints(id string) error {
	var shown bytes.Buffer
	if _, err := (application.PathShow{Tracker: c.tracker, Out: &shown}).Run(context.Background(), id); err != nil {
		return err
	}
	_, after, found := strings.Cut(c.printed.String(), ":\n")
	if !found || after != shown.String() {
		return fmt.Errorf("expected the echo to end with what showing the path prints:\n%s\ngot:\n%s", shown.String(), c.printed.String())
	}
	return nil
}

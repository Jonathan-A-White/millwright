package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// filesExpectedContext holds a fake work tracker with one epic, a real vault
// directory whose rigs/<rig>.toml the scenario writes, and what the last
// command printed.
type filesExpectedContext struct {
	tracker *apptest.FakeTracker
	vault   string
	epic    string
	rig     string

	printed bytes.Buffer
	err     error
}

// InitializeFilesExpectedScenario registers the steps of features/files_expected.feature.
func InitializeFilesExpectedScenario(ctx *godog.ScenarioContext) {
	c := &filesExpectedContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = filesExpectedContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.vault != "" {
			os.RemoveAll(c.vault)
		}
		return ctx, nil
	})

	ctx.Given(`^the files-expected epic "([^"]*)" on the rig "([^"]*)" holds the stories:$`, c.theEpicHoldsTheStories)
	ctx.Given(`^the files-expected story "([^"]*)" is held$`, c.theStoryIsHeld)
	ctx.Given(`^the files-expected epic also holds the story "([^"]*)", on the rig "([^"]*)", with no files$`, c.theEpicAlsoHolds)
	ctx.Given(`^the rig file of "([^"]*)" says:$`, c.theRigFileSays)
	ctx.When(`^mw status reads the host for files$`, c.statusReadsTheHost)
	ctx.When(`^the files-expected epic is released$`, c.theEpicIsReleased)
	ctx.When(`^the files-expected path of "([^"]*)" is shown$`, c.thePathIsShown)
	ctx.Then(`^the files-expected output has the line: (.+)$`, c.outputHasTheLine)
	ctx.Then(`^the files-expected output does not mention: (.+)$`, c.outputDoesNotMention)
	ctx.Then(`^the files-expected story "([^"]*)" is now open$`, c.theStoryIsNowOpen)
}

func (c *filesExpectedContext) theEpicHoldsTheStories(epic, rig string, table *godog.Table) error {
	c.epic, c.rig = epic, rig
	c.tracker.AddEpic(epic, domain.Path{Rig: rig, Branch: "main", Harness: "claude", Model: "sonnet",
		Effort: "high", Formula: "tdd-feature", Host: "vps"})
	for _, row := range table.Rows[1:] {
		story := domain.Story{ID: row.Cells[0].Value, Title: "story " + row.Cells[0].Value}
		story.Overrides.Files = row.Cells[1].Value
		c.tracker.AddStory(epic, story)
	}
	return nil
}

func (c *filesExpectedContext) theStoryIsHeld(id string) error {
	return c.tracker.HoldStory(context.Background(), id)
}

func (c *filesExpectedContext) theEpicAlsoHolds(id, rig string) error {
	story := domain.Story{ID: id, Title: "story " + id}
	story.Overrides.Rig = rig
	c.tracker.AddStory(c.epic, story)
	return c.tracker.HoldStory(context.Background(), id)
}

// theRigFileSays writes the rig's file in a vault of its own.
func (c *filesExpectedContext) theRigFileSays(rig string, written *godog.DocString) error {
	dir, err := c.vaultDir()
	if err != nil {
		return err
	}
	rigs := filepath.Join(dir, application.RigsDir)
	if err := os.MkdirAll(rigs, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rigs, rig+vault.RigFileExt), []byte(written.Content+"\n"), 0o644)
}

func (c *filesExpectedContext) vaultDir() (string, error) {
	if c.vault == "" {
		dir, err := os.MkdirTemp("", "mw-files-expected-")
		if err != nil {
			return "", err
		}
		c.vault = dir
	}
	return c.vault, nil
}

func (c *filesExpectedContext) statusReadsTheHost() error {
	dir, err := c.vaultDir()
	if err != nil {
		return err
	}
	c.printed.Reset()
	_, c.err = application.Status{
		Tracker: c.tracker,
		Vault:   vault.New(dir),
		Rules:   vault.New(dir),
		Host:    "vps",
		Seat:    "builder",
		Out:     &c.printed,
	}.Run(context.Background())
	return c.err
}

func (c *filesExpectedContext) theEpicIsReleased() error {
	dir, err := c.vaultDir()
	if err != nil {
		return err
	}
	c.printed.Reset()
	_, c.err = application.Release{Tracker: c.tracker, Rules: vault.New(dir), Out: &c.printed}.
		Run(context.Background(), c.epic)
	return c.err
}

func (c *filesExpectedContext) thePathIsShown(id string) error {
	dir, err := c.vaultDir()
	if err != nil {
		return err
	}
	c.printed.Reset()
	_, c.err = application.PathShow{Tracker: c.tracker, Rules: vault.New(dir), Out: &c.printed}.
		Run(context.Background(), id)
	return c.err
}

func (c *filesExpectedContext) outputHasTheLine(line string) error {
	for _, got := range strings.Split(c.printed.String(), "\n") {
		if strings.TrimSpace(got) == line {
			return nil
		}
	}
	return fmt.Errorf("expected the output to have the line %q, got:\n%s", line, c.printed.String())
}

func (c *filesExpectedContext) outputDoesNotMention(text string) error {
	if strings.Contains(c.printed.String(), text) {
		return fmt.Errorf("expected the output not to mention %q, got:\n%s", text, c.printed.String())
	}
	return nil
}

func (c *filesExpectedContext) theStoryIsNowOpen(id string) error {
	story, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	if got := application.StateOf(story.Status); got != application.StateOpen {
		return fmt.Errorf("expected %s to be open, it is %s", id, got)
	}
	return nil
}

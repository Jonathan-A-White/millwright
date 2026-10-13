package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// noteContext holds the fake work tracker, the clock the scenario set, and
// what came of noting on a bead.
type noteContext struct {
	tracker *apptest.FakeTracker
	now     time.Time
	dir     string
	file    string

	printed bytes.Buffer
	err     error

	// writesBefore is how many writes the tracker had taken before the last run.
	writesBefore int
}

// InitializeNoteScenario registers the steps of features/note.feature.
func InitializeNoteScenario(ctx *godog.ScenarioContext) {
	c := &noteContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = noteContext{tracker: apptest.NewFakeTracker()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^the note clock reads (\S+)$`, c.theClockReads)
	ctx.Given(`^the note bead "([^"]*)" is open$`, c.aBeadIsOpen)
	ctx.Given(`^a note file holding "([^"]*)"$`, c.aFileHolds)
	ctx.When(`^mw notes on "([^"]*)":(?: (.*))?$`, c.notes)
	ctx.When(`^mw notes on "([^"]*)" with the text from that file$`, c.notesFromTheFile)
	ctx.When(`^mw notes on "([^"]*)" with the text from the file "([^"]*)"$`, c.notesFromFile)
	ctx.Then(`^noting succeeds$`, c.succeeds)
	ctx.Then(`^noting is refused, saying: (.+)$`, c.isRefusedSaying)
	ctx.Then(`^nothing was written by the note command$`, c.nothingWritten)
	ctx.Then(`^the note bead "([^"]*)" has exactly (\d+) comments?, saying "([^"]*)"$`, c.hasComment)
	ctx.Then(`^noting printed "([^"]*)"$`, c.printedSays)
}

func (c *noteContext) theClockReads(stamp string) error {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return err
	}
	c.now = t
	return nil
}

func (c *noteContext) aBeadIsOpen(id string) error {
	c.tracker.AddStory("", domain.Story{ID: id, Title: "bead " + id})
	return nil
}

func (c *noteContext) aFileHolds(text string) error {
	dir, err := os.MkdirTemp("", "mw-note-")
	if err != nil {
		return err
	}
	c.dir = dir
	c.file = filepath.Join(dir, "note.txt")
	return os.WriteFile(c.file, []byte(strings.ReplaceAll(text, `\n`, "\n")), 0o644)
}

func (c *noteContext) run(id, text, path string) error {
	c.printed.Reset()
	c.writesBefore = c.tracker.Writes()
	note := application.Note{
		Tracker: c.tracker,
		Now:     func() time.Time { return c.now },
		Out:     &c.printed,
	}
	if path != "" {
		_, c.err = note.RunFile(context.Background(), id, path)
	} else {
		_, c.err = note.Run(context.Background(), id, text)
	}
	return nil
}

func (c *noteContext) notes(id, text string) error { return c.run(id, text, "") }

func (c *noteContext) notesFromTheFile(id string) error { return c.run(id, "", c.file) }

func (c *noteContext) notesFromFile(id, path string) error { return c.run(id, "", path) }

func (c *noteContext) succeeds() error {
	if c.err != nil {
		return fmt.Errorf("noting failed: %w", c.err)
	}
	return nil
}

func (c *noteContext) isRefusedSaying(reason string) error {
	if c.err == nil {
		return fmt.Errorf("expected noting to be refused, saying %q; it succeeded", reason)
	}
	if !strings.Contains(c.err.Error(), reason) {
		return fmt.Errorf("expected the refusal to say %q, got:\n%s", reason, c.err)
	}
	return nil
}

func (c *noteContext) nothingWritten() error {
	if got := c.tracker.Writes() - c.writesBefore; got != 0 {
		return fmt.Errorf("expected nothing written, the tracker took %d writes", got)
	}
	return nil
}

func (c *noteContext) hasComment(id string, want int, text string) error {
	text = strings.ReplaceAll(text, `\n`, "\n")
	comments := c.tracker.Comments(id)
	if len(comments) != want {
		return fmt.Errorf("expected %d comments on %s, found %d: %q", want, id, len(comments), comments)
	}
	if comments[len(comments)-1] != text {
		return fmt.Errorf("expected the comment on %s to be %q, got %q", id, text, comments[len(comments)-1])
	}
	return nil
}

func (c *noteContext) printedSays(want string) error {
	if !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected the command to print %q, it printed %q", want, c.printed.String())
	}
	return nil
}

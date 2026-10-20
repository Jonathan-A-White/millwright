package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"

	"github.com/cucumber/godog"
)

// bootIdeasClock is the time mw status and mw brief read Ideas only at.
var bootIdeasClock = time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)

// bootIdeasOnly makes the Ideas only a boot report is read with: the state and
// held files of a throwaway host, no backend to ask. until is empty for off.
func bootIdeasOnly(until string, held int) (*application.IdeasOnlyMode, string, error) {
	dir, err := os.MkdirTemp("", "mw-ideas-only-boot-")
	if err != nil {
		return nil, "", err
	}
	files := ideasonly.New(filepath.Join(dir, "ideas-only"))
	state := application.IdeasOnlyState{}
	if until != "" {
		at, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return nil, dir, err
		}
		state = application.IdeasOnlyState{On: true, Until: at.Unix(), Since: bootIdeasClock.Unix()}
	}
	if err := files.SetState(context.Background(), state); err != nil {
		return nil, dir, err
	}
	for n := 1; n <= held; n++ {
		idea := application.IdeasOnlyHeld{Txid: fmt.Sprintf("direct:%d", n), Seq: int64(n), Class: "message", Channel: "general", Text: "an idea"}
		if err := files.Hold(context.Background(), idea); err != nil {
			return nil, dir, err
		}
	}
	return &application.IdeasOnlyMode{Files: files, GovernorKey: "governor-key", Now: func() time.Time { return bootIdeasClock }}, dir, nil
}

// lineBeginning reports whether any line of text begins with prefix.
func lineBeginning(text, prefix string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func (c *statusContext) registerIdeasOnlySteps(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.ideasDir != "" {
			os.RemoveAll(c.ideasDir)
		}
		return ctx, nil
	})
	ctx.Given(`^the status report knows the Governor's Ideas only is on until "([^"]*)" with (\d+) ideas held$`, func(until string, held int) (err error) {
		c.ideas, c.ideasDir, err = bootIdeasOnly(until, held)
		return err
	})
	ctx.Given(`^the status report knows the Governor's Ideas only is off$`, func() (err error) {
		c.ideas, c.ideasDir, err = bootIdeasOnly("", 0)
		return err
	})
	ctx.Then(`^the status report has a line "([^"]*)" right under its title$`, func(line string) error {
		lines := strings.Split(c.report.String(), "\n")
		if len(lines) < 2 || lines[1] != line {
			return fmt.Errorf("expected %q right under the title, got:\n%s", line, c.report.String())
		}
		return nil
	})
	ctx.Then(`^the status report has the line "([^"]*)" under it$`, func(line string) error {
		lines := strings.Split(c.report.String(), "\n")
		if len(lines) < 3 || lines[2] != line {
			return fmt.Errorf("expected %q under the Ideas only line, got:\n%s", line, c.report.String())
		}
		return nil
	})
	ctx.Then(`^the status report has no line beginning "([^"]*)"$`, func(prefix string) error {
		if lineBeginning(c.report.String(), prefix) {
			return fmt.Errorf("expected no line beginning %q, got:\n%s", prefix, c.report.String())
		}
		return nil
	})
}

func (c *briefContext) registerIdeasOnlySteps(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.ideasDir != "" {
			os.RemoveAll(c.ideasDir)
		}
		return ctx, nil
	})
	ctx.Given(`^the brief knows the Governor's Ideas only is on until "([^"]*)" with (\d+) ideas? held$`, func(until string, held int) (err error) {
		c.ideas, c.ideasDir, err = bootIdeasOnly(until, held)
		return err
	})
	ctx.Given(`^the brief knows the Governor's Ideas only is off$`, func() (err error) {
		c.ideas, c.ideasDir, err = bootIdeasOnly("", 0)
		return err
	})
	ctx.Then(`^the brief has no line beginning "([^"]*)"$`, func(prefix string) error {
		if lineBeginning(c.printed, prefix) {
			return fmt.Errorf("expected no line beginning %q, got:\n%s", prefix, c.printed)
		}
		return nil
	})
}

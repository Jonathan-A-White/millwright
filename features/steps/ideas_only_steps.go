package steps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"

	"github.com/cucumber/godog"
)

// ideasOnlyContext is a host's files of Ideas only, in a temp dir, and a fake
// backend that answers the route.
type ideasOnlyContext struct {
	dir     string
	files   *ideasonly.Files
	backend *apptest.FakePostern
	now     time.Time
	out     bytes.Buffer
	errOut  bytes.Buffer
	err     error
}

const ideasOnlyGovernorKey = "governor-key"

func (c *ideasOnlyContext) mode() application.IdeasOnlyMode {
	return application.IdeasOnlyMode{
		Reader: c.backend, Files: c.files, GovernorKey: ideasOnlyGovernorKey,
		Now: func() time.Time { return c.now },
	}
}

// InitializeIdeasOnlyScenario registers the steps of features/ideas_only.feature.
func InitializeIdeasOnlyScenario(ctx *godog.ScenarioContext) {
	c := &ideasOnlyContext{}

	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		*c = ideasOnlyContext{}
		return ctx, nil
	})

	ctx.Given(`^a host that has no note of the Governor's Ideas only$`, func() error {
		dir, err := os.MkdirTemp("", "mw-ideas-only-")
		if err != nil {
			return err
		}
		c.dir = dir
		c.files = ideasonly.New(filepath.Join(dir, "ideas-only"))
		c.backend = apptest.NewFakePostern()
		return nil
	})
	ctx.Given(`^the clock of Ideas only stands at "([^"]*)"$`, func(at string) error {
		var err error
		c.now, err = time.Parse(time.RFC3339, at)
		return err
	})
	ctx.Given(`^the backend says the Governor's Ideas only is off$`, func() error {
		c.backend.IdeasOnlyNow = application.IdeasOnlyNow{}
		return nil
	})
	ctx.Given(`^the backend says the Governor's Ideas only is on until "([^"]*)"$`, func(until string) error {
		return c.backendSaysOn(until, "")
	})
	ctx.Given(`^the backend says the Governor's Ideas only is on until "([^"]*)", on a schedule in "([^"]*)"$`, c.backendSaysOn)
	ctx.Given(`^the backend does not answer the Ideas only route$`, func() error {
		c.backend.IdeasOnlyErr = errors.New("the postern backend said 404")
		return nil
	})
	ctx.Given(`^the note of Ideas only says it is on until "([^"]*)"$`, func(until string) error {
		at, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return err
		}
		return c.files.SetState(context.Background(), application.IdeasOnlyState{On: true, Until: at.Unix()})
	})
	ctx.Given(`^Ideas only is holding "([^"]*)" sent in "([^"]*)" saying "([^"]*)"$`, func(txid, channel, text string) error {
		return c.files.Hold(context.Background(), application.IdeasOnlyHeld{
			Txid: txid, Seq: 1, Class: "message", Channel: channel, Text: text, When: c.now.Format(time.RFC3339),
		})
	})

	ctx.When(`^mw ideas-only is run$`, func() error {
		c.out.Reset()
		c.errOut.Reset()
		_, c.err = application.IdeasOnlyShow{Mode: c.mode(), Out: &c.out, Err: &c.errOut}.Run(context.Background())
		return nil
	})

	ctx.Then(`^Ideas only printed "([^"]*)"$`, func(text string) error { return c.printed(text, true) })
	ctx.Then(`^Ideas only did not print "([^"]*)"$`, func(text string) error { return c.printed(text, false) })
	ctx.Then(`^Ideas only said "([^"]*)"$`, func(text string) error {
		if !strings.Contains(c.errOut.String(), text) {
			return fmt.Errorf("expected mw ideas-only to say %q, it said:\n%s", text, c.errOut.String())
		}
		return nil
	})
	ctx.Then(`^the note of Ideas only says it is on until "([^"]*)"$`, func(until string) error {
		at, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return err
		}
		state, err := c.files.State(context.Background())
		if err != nil {
			return err
		}
		if !state.On || state.Until != at.Unix() {
			return fmt.Errorf("expected the note to say on until %d, it says %+v", at.Unix(), state)
		}
		return nil
	})
}

func (c *ideasOnlyContext) backendSaysOn(until, tz string) error {
	at, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return err
	}
	c.backend.IdeasOnlyNow = application.IdeasOnlyNow{On: true, Until: at.Unix(), By: application.IdeasOnlyByUntil, Tz: tz}
	return nil
}

func (c *ideasOnlyContext) printed(text string, want bool) error {
	if c.err != nil {
		return fmt.Errorf("mw ideas-only failed: %w", c.err)
	}
	if got := strings.Contains(c.out.String(), text); got != want {
		if want {
			return fmt.Errorf("expected mw ideas-only to print %q, it printed:\n%s", text, c.out.String())
		}
		return fmt.Errorf("expected mw ideas-only not to print %q, it printed:\n%s", text, c.out.String())
	}
	return nil
}

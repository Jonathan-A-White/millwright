package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// homeContext holds a fake home file for mw home to read. Nothing here touches
// a vault directory, beads or git.
type homeContext struct {
	files *apptest.FakeHomeFile
	host  string

	out    string // what goes to stdout
	errs   string // what goes to stderr
	status int
}

// InitializeHomeScenario registers the steps of features/home.feature.
func InitializeHomeScenario(ctx *godog.ScenarioContext) {
	c := &homeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = homeContext{files: &apptest.FakeHomeFile{}}
		return ctx, nil
	})

	ctx.Given(`^the home file of the vault reads "([^"]*)"$`, c.theHomeFileReads)
	ctx.Given(`^the vault has no home file$`, c.theVaultHasNoHomeFile)
	ctx.Given(`^this host, for mw home, is "([^"]*)"$`, c.thisHostIs)

	ctx.When(`^mw home runs$`, func() error { return c.run(false) })
	ctx.When(`^mw home --check runs$`, func() error { return c.run(true) })

	ctx.Then(`^mw home prints:$`, c.mwHomePrints)
	ctx.Then(`^mw home prints nothing$`, c.mwHomePrintsNothing)
	ctx.Then(`^mw home prints only the line "([^"]*)"$`, c.mwHomePrintsOnlyTheLine)
	ctx.Then(`^mw home says "([^"]*)"$`, c.mwHomeSays)
	ctx.Then(`^mw home leaves with the status (\d+)$`, c.mwHomeLeavesWith)
}

func (c *homeContext) theHomeFileReads(text string) error {
	c.files.Text = text + "\n"
	return nil
}

func (c *homeContext) theVaultHasNoHomeFile() error {
	c.files.Missing = true
	return nil
}

func (c *homeContext) thisHostIs(host string) error {
	c.host = host
	return nil
}

func (c *homeContext) run(check bool) error {
	home := application.Home{Files: c.files, Host: c.host}
	run := home.Run
	if check {
		run = home.Check
	}
	report, err := run(context.Background())
	c.out = report.String()
	c.status = application.ExitStatus(err)
	if err != nil {
		c.errs = err.Error() + "\n"
	}
	return nil
}

func (c *homeContext) mwHomePrints(want *godog.DocString) error {
	if got := strings.TrimSpace(c.out); got != strings.TrimSpace(want.Content) {
		return fmt.Errorf("mw home printed:\n%s\nwanted:\n%s", got, want.Content)
	}
	return nil
}

func (c *homeContext) mwHomePrintsNothing() error {
	if c.out != "" {
		return fmt.Errorf("mw home printed %q on stdout, wanted nothing", c.out)
	}
	return nil
}

func (c *homeContext) mwHomePrintsOnlyTheLine(want string) error {
	if c.out != want+"\n" {
		return fmt.Errorf("mw home printed %q on stdout, wanted exactly %q", c.out, want+"\n")
	}
	return nil
}

func (c *homeContext) mwHomeSays(want string) error {
	if !strings.Contains(c.errs, want) {
		return fmt.Errorf("mw home said %q on stderr, which has no %q", c.errs, want)
	}
	return nil
}

func (c *homeContext) mwHomeLeavesWith(want int) error {
	if c.status != want {
		return fmt.Errorf("mw home left with status %d, wanted %d (it said %q)", c.status, want, c.errs)
	}
	return nil
}

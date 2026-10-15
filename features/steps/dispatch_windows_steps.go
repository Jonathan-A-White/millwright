package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/cucumber/godog"
)

// dispatchWindowsContext holds a home directory of its own, with the config
// file a scenario wrote, and the instant its clock reads.
type dispatchWindowsContext struct {
	now time.Time
}

// InitializeDispatchWindowsScenario registers the steps of
// features/dispatch_windows.feature. It isolates HOME lazily, inside the steps
// that need it: the hooks of every feature run for every scenario.
func InitializeDispatchWindowsScenario(ctx *godog.ScenarioContext) {
	c := &dispatchWindowsContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = dispatchWindowsContext{}
		return ctx, nil
	})

	ctx.Given(`^a host config file holding:$`, c.aHostConfigFileHolding)
	ctx.Given(`^the environment says MW_CAP is (\d+)$`, c.theEnvironmentSaysCap)
	ctx.When(`^the clock reads "([^"]*)" in ([A-Za-z_/]+)$`, c.theClockReads)
	ctx.When(`^the instant is "([^"]*)" UTC$`, c.theInstantIs)
	ctx.Then(`^the cap in force is (\d+)$`, c.theCapInForceIs)
	ctx.Then(`^builder_jobs in force is (\d+)$`, c.builderJobsInForceIs)
	ctx.Then(`^builder_nice in force is (\d+)$`, c.builderNiceInForceIs)
	ctx.Then(`^the room needs at least (\d+) MB free$`, c.theRoomNeedsMB)
	ctx.Then(`^the room allows a load of ([0-9.]+) a core$`, c.theRoomAllowsLoad)
	ctx.Then(`^the cap is explained as "([^"]*)"$`, c.theCapIsExplainedAs)
	ctx.Then(`^the windows are refused, naming the key (\w+)$`, c.theWindowsAreRefused)
	ctx.Then(`^the windows are not refused$`, c.theWindowsAreNotRefused)
}

func (c *dispatchWindowsContext) aHostConfigFileHolding(doc *godog.DocString) error {
	home, err := os.MkdirTemp("", "mw-dispatch-windows-")
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The scenario's own HOME, and no variable that would answer ahead of the file.
	os.Setenv("HOME", home)
	for _, name := range []string{config.CapEnv, config.BuilderJobsEnv, config.BuilderNiceEnv} {
		os.Unsetenv(name)
	}
	c.now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	return os.WriteFile(filepath.Join(dir, "config.toml"), []byte(doc.Content+"\n"), 0o600)
}

func (c *dispatchWindowsContext) theEnvironmentSaysCap(n string) error {
	return os.Setenv(config.CapEnv, n)
}

func (c *dispatchWindowsContext) theClockReads(local, zone string) error {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	at, err := time.ParseInLocation("2006-01-02 15:04", local, loc)
	if err != nil {
		return err
	}
	c.now = at
	return nil
}

func (c *dispatchWindowsContext) theInstantIs(instant string) error {
	at, err := time.Parse(time.RFC3339, instant)
	if err != nil {
		return err
	}
	c.now = at
	return nil
}

func (c *dispatchWindowsContext) theCapInForceIs(want int) error {
	got, err := config.CapAt(c.now)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("the cap in force at %s is %d, want %d", c.now.Format(time.RFC3339), got, want)
	}
	return nil
}

func (c *dispatchWindowsContext) builderJobsInForceIs(want int) error {
	got, err := config.BuilderJobsAt(c.now)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("builder_jobs in force at %s is %d, want %d", c.now.Format(time.RFC3339), got, want)
	}
	return nil
}

func (c *dispatchWindowsContext) builderNiceInForceIs(want int) error {
	got, err := config.BuilderNiceAt(c.now)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("builder_nice in force at %s is %d, want %d", c.now.Format(time.RFC3339), got, want)
	}
	return nil
}

func (c *dispatchWindowsContext) theRoomNeedsMB(want int64) error {
	room, err := config.RoomAt(c.now)
	if err != nil {
		return err
	}
	if room.MinFreeMB != want {
		return fmt.Errorf("the room needs %d MB free at %s, want %d", room.MinFreeMB, c.now.Format(time.RFC3339), want)
	}
	return nil
}

func (c *dispatchWindowsContext) theRoomAllowsLoad(want float64) error {
	room, err := config.RoomAt(c.now)
	if err != nil {
		return err
	}
	if room.LoadPerCore != want {
		return fmt.Errorf("the room allows a load of %v a core at %s, want %v", room.LoadPerCore, c.now.Format(time.RFC3339), want)
	}
	return nil
}

func (c *dispatchWindowsContext) theCapIsExplainedAs(want string) error {
	_, why, err := config.CapInForce(c.now)
	if err != nil {
		return err
	}
	if why != want {
		return fmt.Errorf("the cap is explained as %q, want %q", why, want)
	}
	return nil
}

func (c *dispatchWindowsContext) theWindowsAreRefused(key string) error {
	saying := key + " = "
	problems := config.DispatchWindowProblems()
	if len(problems) == 0 {
		return fmt.Errorf("no window was refused, want one saying %q", saying)
	}
	var all []string
	for _, problem := range problems {
		all = append(all, problem.Error())
	}
	if text := strings.Join(all, "\n"); !strings.Contains(text, saying) {
		return fmt.Errorf("the windows were refused saying %q, want it to say %q", text, saying)
	}
	return nil
}

func (c *dispatchWindowsContext) theWindowsAreNotRefused() error {
	if problems := config.DispatchWindowProblems(); len(problems) != 0 {
		return fmt.Errorf("the windows were refused: %v", problems)
	}
	return nil
}

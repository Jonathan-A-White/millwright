package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// builderJobsContext holds a home directory of its own, with the config file a
// scenario wrote, and what the rig's tests printed.
type builderJobsContext struct {
	home    string
	printed string
	err     error
}

// InitializeBuilderJobsScenario registers the steps of features/builder_jobs.feature.
// It isolates HOME lazily, inside the steps that need it: the hooks of every
// feature run for every scenario.
func InitializeBuilderJobsScenario(ctx *godog.ScenarioContext) {
	c := &builderJobsContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = builderJobsContext{}
		return ctx, nil
	})

	ctx.Given(`^a config file on this host holding:$`, c.aConfigFileHolding)
	ctx.When(`^the rig's tests are run on this host with a command that prints its worker limits$`, c.testsAreRun)
	ctx.Then(`^the tests printed: (.+)$`, c.testsPrinted)
	ctx.Then(`^reading builder_jobs is refused, saying: (.+)$`, c.readingIsRefused)
}

func (c *builderJobsContext) aConfigFileHolding(doc *godog.DocString) error {
	home, err := os.MkdirTemp("", "mw-builder-jobs-")
	if err != nil {
		return err
	}
	c.home = home
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// The scenario's own HOME, and no variable that would answer ahead of the file.
	os.Setenv("HOME", home)
	for _, name := range []string{config.BuilderJobsEnv, "JOBS", "GOMAXPROCS", "VITEST_MAX_WORKERS"} {
		os.Unsetenv(name)
	}
	return os.WriteFile(filepath.Join(dir, "config.toml"), []byte(doc.Content+"\n"), 0o600)
}

func (c *builderJobsContext) testsAreRun() error {
	jobs, err := config.BuilderJobs()
	if err != nil {
		return err
	}
	checks := rig.NewChecks(
		rig.WithCommand(`echo "JOBS=$JOBS GOMAXPROCS=$GOMAXPROCS VITEST_MAX_WORKERS=$VITEST_MAX_WORKERS"`),
		rig.WithJobs(jobs))
	checked, err := checks.Run(context.Background(), "millwright", c.home)
	if err != nil {
		return err
	}
	c.printed = strings.TrimSpace(checked.Output)
	return nil
}

func (c *builderJobsContext) testsPrinted(want string) error {
	if c.printed != want {
		return fmt.Errorf("the tests printed %q, want %q", c.printed, want)
	}
	return nil
}

func (c *builderJobsContext) readingIsRefused(saying string) error {
	_, err := config.BuilderJobs()
	if err == nil {
		return fmt.Errorf("builder_jobs was taken, want a refusal saying %q", saying)
	}
	if !strings.Contains(err.Error(), saying) {
		return fmt.Errorf("refused saying %q, want it to say %q", err, saying)
	}
	return nil
}

package features

import (
	"os"
	"testing"

	"github.com/Jonathan-A-White/millwright/features/steps"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

// MW_FEATURE narrows a run to one feature file, or one scenario within it,
// without touching `make test` (which leaves it unset and so still runs
// every feature). The value is a path relative to this directory, exactly
// as godog's own CLI accepts it: "sweep.feature" for one feature, or
// "sweep.feature:17" for the one scenario starting at that line. godog
// resolves the path itself and fails the suite (non-zero, caught below) if
// it does not exist, so an unknown feature name cannot pass with zero
// scenarios.
func TestFeatures(t *testing.T) {
	paths := []string{"./"}
	if f := os.Getenv("MW_FEATURE"); f != "" {
		paths = []string{f}
	}

	opts := godog.Options{
		Format:   "pretty",
		Output:   colors.Colored(os.Stdout),
		Paths:    paths,
		Strict:   true,
		TestingT: t,
	}

	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenarios,
		Options:             &opts,
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

// initializeScenarios registers the step definitions of every feature.
func initializeScenarios(ctx *godog.ScenarioContext) {
	steps.InitializeBriefScenario(ctx)
	steps.InitializeDispatchScenario(ctx)
	steps.InitializeFilePlanScenario(ctx)
	steps.InitializeMailBeadsScenario(ctx)
	steps.InitializeMailScenario(ctx)
	steps.InitializeNextScenario(ctx)
	steps.InitializePathScenario(ctx)
	steps.InitializeReadyStoriesScenario(ctx)
	steps.InitializeReleaseScenario(ctx)
	steps.InitializeSeatBootScenario(ctx)
	steps.InitializeSeatContextScenario(ctx)
	steps.InitializeSeatReapScenario(ctx)
	steps.InitializeSeatUpScenario(ctx)
	steps.InitializeStatusScenario(ctx)
	steps.InitializeSweepScenario(ctx)
	steps.InitializeSyncScenario(ctx)
}

package features

import (
	"os"
	"testing"

	"github.com/Jonathan-A-White/millwright/features/steps"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

func TestFeatures(t *testing.T) {
	opts := godog.Options{
		Format:   "pretty",
		Output:   colors.Colored(os.Stdout),
		Paths:    []string{"./"},
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
	steps.InitializeDispatchScenario(ctx)
	steps.InitializeFilePlanScenario(ctx)
	steps.InitializeNextScenario(ctx)
	steps.InitializePathScenario(ctx)
	steps.InitializeReadyStoriesScenario(ctx)
	steps.InitializeReleaseScenario(ctx)
	steps.InitializeSeatBootScenario(ctx)
	steps.InitializeSyncScenario(ctx)
}

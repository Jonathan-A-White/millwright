package features

import (
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/features/steps"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

// TestMain clears bd's Dolt server settings before any scenario runs. A session
// on a host whose bd runs in server mode carries them (beads.env), and the real
// `bd init` some scenarios run would then make its throwaway database on the
// host's shared server instead of in the scenario's temp directory.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "BEADS_DOLT_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

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
	steps.InitializeDoctorScenario(ctx)
	steps.InitializeFilePlanScenario(ctx)
	steps.InitializeGristEvalScenario(ctx)
	steps.InitializeGristScenario(ctx)
	steps.InitializeGristSendScenario(ctx)
	steps.InitializeHandsScenario(ctx)
	steps.InitializeHomeScenario(ctx)
	steps.InitializeInitScenario(ctx)
	steps.InitializeMailBeadsScenario(ctx)
	steps.InitializeMailScenario(ctx)
	steps.InitializeNextScenario(ctx)
	steps.InitializePathScenario(ctx)
	steps.InitializePosternBeadScenario(ctx)
	steps.InitializePosternInboxScenario(ctx)
	steps.InitializePosternKeyScenario(ctx)
	steps.InitializePosternSendScenario(ctx)
	steps.InitializePosternServeScenario(ctx)
	steps.InitializePosternSnapshotScenario(ctx)
	steps.InitializePosternViewScenario(ctx)
	steps.InitializeReadyStoriesScenario(ctx)
	steps.InitializeReleaseScenario(ctx)
	steps.InitializeRetryScenario(ctx)
	steps.InitializeSeatBootScenario(ctx)
	steps.InitializeSeatContextScenario(ctx)
	steps.InitializeSeatReapScenario(ctx)
	steps.InitializeSeatUpScenario(ctx)
	steps.InitializeStatusScenario(ctx)
	steps.InitializeSweepScenario(ctx)
	steps.InitializeTidyScenario(ctx)
	steps.InitializeWatchScenario(ctx)
	steps.InitializeSyncScenario(ctx)
}

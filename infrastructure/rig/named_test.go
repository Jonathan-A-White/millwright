package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestRunNamedPutsTheTestNamesIntoTheRigsLine(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "given.txt")
	checks := rig.NewChecks(rig.WithNamedCommands(map[string]string{
		"millwright": "printf '%s\\n' {tests} > " + log + "; echo '--- FAIL: TestA'; exit 1",
	}))

	run, err := checks.RunNamed(context.Background(), "millwright", dir, []string{"TestA", "src/a.test.ts > does a thing"})
	if err != nil {
		t.Fatal(err)
	}
	if !run.Ran || run.Passed || !strings.Contains(run.Output, "--- FAIL: TestA") {
		t.Errorf("expected a failed run of the line, got %+v", run)
	}
	given, _ := os.ReadFile(log)
	if want := "TestA\nsrc/a.test.ts > does a thing\n"; string(given) != want {
		t.Errorf("the line was given %q, want %q", given, want)
	}
}

func TestRunNamedRunsNothingForARigWithNoLineOrNoNames(t *testing.T) {
	checks := rig.NewChecks(rig.WithNamedCommands(map[string]string{"millwright": "exit 1"}))
	for _, c := range []struct {
		rig   string
		tests []string
	}{{"another", []string{"TestA"}}, {"millwright", nil}} {
		run, err := checks.RunNamed(context.Background(), c.rig, t.TempDir(), c.tests)
		if err != nil || run.Ran {
			t.Errorf("RunNamed(%q, %q) = %+v, %v; want nothing run", c.rig, c.tests, run, err)
		}
	}
}

// millwrightNamedLine is the [named_tests] line of the millwright rig, as the
// config file's reader hands it on (mw-gq6.947).
const millwrightNamedLine = `go test -tags beads_integration -run "^($(echo {tests} | tr ' ' '|'))$" ./...`

// aSmallGoPackage is a directory with a module whose tests are the named ones;
// those in failing fail.
func aSmallGoPackage(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"go.mod": "module small\n\ngo 1.21\n", "small_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The output of the gate of mw-gq6.943 names one failing test and, a few lines
// below it, the package go test printed as FAIL<tab><package>. The test is run
// alone by the millwright rig's line and nothing else is.
func TestRunNamedRunsTheFailingTestOfARealGateLogAloneThroughTheMillwrightLine(t *testing.T) {
	gate, err := os.ReadFile(filepath.Join("testdata", "gate-mw-gq6.943.log"))
	if err != nil {
		t.Fatal(err)
	}
	names := application.TestNamesOf(string(gate))
	want := "TestOnlyWhatChangedTravelsAFileThatDidNotIsLinkedToTheLiveOne"
	if !reflect.DeepEqual(names, []string{want}) {
		t.Fatalf("TestNamesOf = %q, want just %q", names, want)
	}
	dir := aSmallGoPackage(t, `package small

import "testing"

func `+want+`(t *testing.T) { t.Fatal("still red") }
func TestSomethingElse(t *testing.T) { t.Fatal("not asked for") }
`)
	checks := rig.NewChecks(rig.WithNamedCommands(map[string]string{"millwright": millwrightNamedLine}))

	run, err := checks.RunNamed(context.Background(), "millwright", dir, names)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Ran || run.Passed {
		t.Fatalf("expected the named test run and failed, got %+v", run.Checked)
	}
	if got := application.TestNamesOf(run.Output); !reflect.DeepEqual(got, []string{want}) {
		t.Errorf("the run named %q, want just %q; output:\n%s", got, want, run.Output)
	}
}

// A subtest that failed is its top-level test to the line, and a package that
// does not build ends the run with a plain answer, not a hang or an error.
func TestRunNamedThroughTheMillwrightLineAnswersASubtestAndABrokenBuild(t *testing.T) {
	checks := rig.NewChecks(rig.WithNamedCommands(map[string]string{"millwright": millwrightNamedLine}))

	subtests := aSmallGoPackage(t, `package small

import "testing"

func TestTable(t *testing.T) {
	t.Run("case", func(t *testing.T) { t.Fatal("red") })
}
`)
	output := "--- FAIL: TestTable (0.00s)\n    --- FAIL: TestTable/case (0.00s)\nFAIL\nFAIL\tsmall\t0.01s\n"
	names := application.TestNamesOf(output)
	if !reflect.DeepEqual(names, []string{"TestTable"}) {
		t.Fatalf("TestNamesOf = %q, want TestTable", names)
	}
	run, err := checks.RunNamed(context.Background(), "millwright", subtests, names)
	if err != nil || !run.Ran || run.Passed {
		t.Fatalf("expected a failed run of TestTable, got %+v, %v", run.Checked, err)
	}
	if got := application.TestNamesOf(run.Output); !reflect.DeepEqual(got, []string{"TestTable"}) {
		t.Errorf("the run named %q, want TestTable; output:\n%s", got, run.Output)
	}

	broken := aSmallGoPackage(t, "package small\n\nimport \"testing\"\n\nfunc TestTable(t *testing.T) { undefined() }\n")
	run, err = checks.RunNamed(context.Background(), "millwright", broken, names)
	if err != nil || !run.Ran || run.Passed {
		t.Fatalf("expected a failed run of a package that does not build, got %+v, %v", run.Checked, err)
	}
	if got := application.TestNamesOf(run.Output); len(got) != 0 {
		t.Errorf("a build that broke named tests %q", got)
	}
}

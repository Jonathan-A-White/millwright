package rig_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

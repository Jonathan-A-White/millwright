package features

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runNarrowed runs TestFeatures in a child process of this test binary with
// MW_FEATURE set to value, and returns its combined output and whether it passed.
func runNarrowed(t *testing.T, value string) (string, bool) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("finding this test binary: %v", err)
	}
	child := exec.Command(self, "-test.run=^TestFeatures$", "-test.count=1")
	child.Env = append(os.Environ(), "MW_FEATURE="+value)
	out, err := child.CombinedOutput()
	return string(out), err == nil
}

// A narrowed run that executes no scenario is a failure, not a pass: a line
// that starts no scenario (a comment, a blank line) must not read as green.
func TestMwFeatureThatRunsNoScenarioFails(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.feature")
	if err := os.WriteFile(empty, []byte("Feature: nothing to run\n\n  # a comment, no scenario\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, ok := runNarrowed(t, empty); ok || !strings.Contains(out, "MW_FEATURE") {
		t.Fatalf("a feature with no scenario passed, or failed without naming MW_FEATURE (passed %v):\n%s", ok, out)
	}

	// A line inside a feature that has a scenario, but is not the line the
	// scenario starts on: a comment above it.
	comment := filepath.Join(dir, "comment.feature")
	body := "Feature: one scenario\n\n  # a comment\n  Scenario: The one\n    Given nothing\n"
	if err := os.WriteFile(comment, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, ok := runNarrowed(t, comment+":3"); ok || !strings.Contains(out, "MW_FEATURE") {
		t.Fatalf("a comment line passed, or failed without naming MW_FEATURE (passed %v):\n%s", ok, out)
	}
}

// The line a scenario starts on still runs it and passes.
func TestMwFeatureAtAScenarioLineStillPasses(t *testing.T) {
	if out, ok := runNarrowed(t, "dispatch.feature:472"); !ok {
		t.Fatalf("dispatch.feature:472 did not pass:\n%s", out)
	}
}

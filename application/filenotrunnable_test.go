package application_test

import (
	"context"
	"strings"
	"testing"
)

// mw-gq6.1383: mw file and mw file --check warn, as mw path set does, of an
// acceptance criterion a Builder cannot run before landing.
func TestFileWarnsOfACriterionThatNeedsTheLiveSite(t *testing.T) {
	acceptance := "curl -s https://postern.allmymind.org/ prints ok\nExpect: none, because a test plan"
	var checkOut, checkWarn, runOut, runWarn strings.Builder
	if err := gristFile(&checkOut, &checkWarn).Check(context.Background(), gristAcceptancePlan(acceptance)); err != nil {
		t.Fatalf("check: expected a warning and not a refusal, got %v", err)
	}
	if _, err := gristFile(&runOut, &runWarn).Run(context.Background(), gristAcceptancePlan(acceptance)); err != nil {
		t.Fatalf("file: expected a warning and not a refusal, got %v", err)
	}
	for name, warn := range map[string]string{"mw file --check": checkWarn.String(), "mw file": runWarn.String()} {
		for _, want := range []string{"WARN:", "criterion 1", "live URL", "after-landing check"} {
			if !strings.Contains(warn, want) {
				t.Errorf("%s: expected the warning to say %q, got %q", name, want, warn)
			}
		}
	}
	// Only the story's label differs: its plan key in Check, its filed id in Run.
	words := func(line string) string { return line[strings.Index(line, ": criterion"):] }
	if line := firstLineWith(checkWarn.String(), "live URL"); line == "" || words(line) != words(firstLineWith(runWarn.String(), "live URL")) {
		t.Errorf("expected the same line from both, got %q and %q", checkWarn.String(), runWarn.String())
	}
}

func TestFileDoesNotWarnOfACriterionACheckCanRunBeforeLanding(t *testing.T) {
	var out, warn strings.Builder
	if err := gristFile(&out, &warn).Check(context.Background(),
		gristAcceptancePlan("go test ./application -run TestX passes\nExpect: none, because a test plan")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(warn.String(), "live URL") {
		t.Errorf("expected no warning, got %q", warn.String())
	}
}

func firstLineWith(text, want string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

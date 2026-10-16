package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestTheLandRushPageNamesTheSevenKeysTheStopRuleAndTheRollback(t *testing.T) {
	raw, err := os.ReadFile("land-rush.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	for _, want := range []string{
		"landing", "batch_every", "batch_min", "batch_max",
		"batch_fixer_model", "batch_fixer_effort", "batch_fixer_turns",
		`landing = "each"`, "## Rollback", "## The stop rule",
		"runs/batch/", "metrics/factory.jsonl", "mw land",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the page does not name %q", want)
		}
	}
}

func TestTheReadmeLinksTheLandRushPage(t *testing.T) {
	raw, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "docs/land-rush.md") {
		t.Error("README.md does not link docs/land-rush.md")
	}
}

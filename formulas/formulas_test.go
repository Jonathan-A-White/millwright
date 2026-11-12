package formulas_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// closingSteps are the step of each formula that asks for a comment on a bead
// at the end (mw-gq6.1045): the Builder's closing comment, the Tester's FINDINGS.
var closingSteps = map[string][]string{
	"tdd-feature.formula.json": {"close", "close-bug"},
	"chore.formula.json":       {"close"},
	"tester.formula.json":      {"findings"},
}

func TestEachFormulasClosingStepSaysToPostTheCommentFromAFileAndReadItBack(t *testing.T) {
	for file, ids := range closingSteps {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var formula struct {
			Steps []struct{ ID, Description string }
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, id := range ids {
			found := false
			for _, step := range formula.Steps {
				if step.ID != id {
					continue
				}
				found = true
				for _, want := range []string{"`bd comments add <id> -f <file>`", "read it back with `bd show <id>`", "never from a heredoc on stdin"} {
					if !strings.Contains(step.Description, want) {
						t.Errorf("%s step %s: expected its text to hold %q", file, id, want)
					}
				}
			}
			if !found {
				t.Errorf("%s: no step %q", file, id)
			}
		}
	}
}

// A Builder's closing step names the heading mw next reads its findings under
// (mw-t5shinh.24): the tester's FINDINGS step has its own sections.
func TestEachBuildersClosingStepAsksForTheFoundNotFixedSection(t *testing.T) {
	for file, ids := range closingSteps {
		if file == "tester.formula.json" {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var formula struct {
			Steps []struct{ ID, Description string }
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, id := range ids {
			for _, step := range formula.Steps {
				if step.ID == id && !strings.Contains(step.Description, "a section headed exactly 'Found, not fixed:'") {
					t.Errorf("%s step %s: expected it to ask for a section headed exactly 'Found, not fixed:'", file, id)
				}
				// Each finding carries an AC, a Sweep line, a size and a by-design answer
				// (mw-gq6.1124).
				if step.ID == id {
					for _, ask := range []string{"'AC: <", "'Sweep: <", "'Size: <", "'By design? yes'", "no AC from the Builder"} {
						if !strings.Contains(step.Description, ask) {
							t.Errorf("%s step %s: expected it to ask each finding for %s", file, id, ask)
						}
					}
				}
			}
		}
	}
}

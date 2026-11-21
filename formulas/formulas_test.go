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

// The gate step does not say to hand back at the wait limit: mw check names the
// step and says to close it as left to the close-out (mw-gq6.1557, mw-gq6.1563).
func TestTheGateStepSaysWhatToDoOnTheWaitLimitLine(t *testing.T) {
	for _, tc := range []struct{ file string }{
		{"tdd-feature.formula.json"},
		{"chore.formula.json"},
	} {
		raw, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		var formula struct {
			Steps []struct{ ID, Description string }
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		found := false
		for _, step := range formula.Steps {
			if !strings.Contains(step.Description, "wait limit was reached") {
				continue
			}
			found = true
			if !strings.Contains(step.Description, "gate left to the close-out") {
				t.Errorf("%s step %s: expected it to say to close the gate step as 'gate left to the close-out'", tc.file, step.ID)
			}
			if strings.Contains(step.Description, "Hand back only if a line says the wait limit") {
				t.Errorf("%s step %s: expected it not to say to hand back at the wait limit", tc.file, step.ID)
			}
		}
		if !found {
			t.Errorf("%s: no step names the wait limit line", tc.file)
		}
	}
}

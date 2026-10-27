package application_test

import (
	"encoding/json"
	"fmt"
	"github.com/Jonathan-A-White/millwright/application"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Builder that backgrounds its gate and waits on it with tail --pid is
// refused by the auto-mode classifier and its story stops with steps open
// (mw-gq6.482): every formula step that runs the rig's tests names the
// foreground way and says not to background and wait.
func TestTheGateStepOfEachFormulaNamesTheForegroundWay(t *testing.T) {
	for _, tc := range []struct{ file, step string }{
		{"tdd-feature.formula.json", "verify"},
		{"chore.formula.json", "verify"},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "formulas", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		var formula struct {
			Steps []formulaStep `json:"steps"`
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		found := false
		for _, s := range formula.Steps {
			if s.ID != tc.step {
				continue
			}
			found = true
			for _, want := range []string{"timeout 590 make test", "mw check {{story}}", "mw check {{story}} --part N", "Never nohup", "tail --pid", "sleep"} {
				if !strings.Contains(s.Description, want) {
					t.Errorf("%s step %q: expected the description to contain %q, got: %s", tc.file, tc.step, want, s.Description)
				}
			}
		}
		if !found {
			t.Errorf("%s: no step %q", tc.file, tc.step)
		}
	}
}

// A part that could not start for the merge slot is not waited on in a
// background loop (mw-6zgnz5y): the gate step says to run it once more and then
// hand back naming the unrun parts and the command that runs each.
func TestTheGateStepSaysWhatToDoWhenAPartCannotGetTheMergeSlot(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "formulas", "tdd-feature.formula.json"))
	if err != nil {
		t.Fatal(err)
	}
	var formula struct {
		Steps []formulaStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &formula); err != nil {
		t.Fatal(err)
	}
	for _, s := range formula.Steps {
		if s.ID != "verify" {
			continue
		}
		for _, want := range []string{"the merge slot is held", "did not start", "do not start a background loop", "naming each part not yet run", "hand back", "Never stop with steps open and no comment", "waits up to " + gateStepMinutes(application.CheckPartSlotWait)} {
			if !strings.Contains(s.Description, want) {
				t.Errorf("expected the gate step to contain %q, got: %s", want, s.Description)
			}
		}
		return
	}
	t.Fatal("no verify step in tdd-feature")
}

// A Builder that met an 'under load' acceptance criterion started sixteen busy
// loops on the home host and held dispatch, close-outs and a batch for calm
// (mw-gq6.483): each formula that has a Builder or Tester check things names
// the one bounded way, contrib/mw-under-load, and forbids the hand-rolled one.
func TestEachFormulaNamesTheBoundedWayToCheckUnderLoad(t *testing.T) {
	for _, tc := range []struct{ file, step string }{
		{"tdd-feature.formula.json", "verify"},
		{"chore.formula.json", "verify"},
		{"tester.formula.json", "adversarial"},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "formulas", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		var formula struct {
			Steps []formulaStep `json:"steps"`
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		found := false
		for _, s := range formula.Steps {
			if s.ID != tc.step {
				continue
			}
			found = true
			for _, want := range []string{"'under load'", "contrib/mw-under-load", "never hand-rolled busy loops, stress or yes"} {
				if !strings.Contains(s.Description, want) {
					t.Errorf("%s step %q: expected the description to contain %q, got: %s", tc.file, tc.step, want, s.Description)
				}
			}
		}
		if !found {
			t.Errorf("%s: no step %q", tc.file, tc.step)
		}
	}
}

// A rig whose whole gate outruns a foreground tool call is checked one part per
// call (mw-gq6.567): the Builder's kickoff says how, in the foreground, in place
// of a background wait.
func TestTheBuilderKickoffNamesTheGateInPartsForASplitRig(t *testing.T) {
	kickoff := application.KickoffPrompt("builder", "mw-gq6.6", "/vault", nil)
	for _, want := range []string{"is split into parts", "`mw check mw-gq6.6 --part N`", "own foreground call", "`mw check mw-gq6.6` again for the verdict"} {
		if !strings.Contains(kickoff, want) {
			t.Errorf("expected the builder kickoff to contain %q, got: %s", want, kickoff)
		}
	}
}

// gateStepMinutes is a wait as the gate step says it, so that the step and the
// bound it describes cannot drift apart.
func gateStepMinutes(d time.Duration) string {
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

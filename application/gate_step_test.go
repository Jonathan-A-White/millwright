package application_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			for _, want := range []string{"timeout 590 make test", "mw check {{story}}", "Never nohup", "tail --pid", "sleep"} {
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

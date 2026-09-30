package application_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A Builder proposes rig-memory notes in its closing comment and never writes
// the rig's memory file: the formulas' closing steps and both kickoff prompts
// say so, in the same words.
const proposeNotesHeading = "For the rig memory:"

// The closing comment also tells the Governor how to check what he can see,
// ahead of the rig-memory section.
const (
	howToCheckHeading  = "HOW TO CHECK IT, for the Governor"
	howToCheckInternal = "Internal: nothing for the Governor to look at"
)

func TestClosingStepOfEachFormulaTellsTheBuilderToProposeRigMemoryNotes(t *testing.T) {
	for _, tc := range []struct{ file, step string }{
		{"tdd-feature.formula.json", "close"},
		{"chore.formula.json", "close"},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "formulas", tc.file))
		if err != nil {
			t.Fatalf("reading %s: %v", tc.file, err)
		}
		var formula struct {
			Steps []struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("decoding %s: %v", tc.file, err)
		}
		found := false
		for _, s := range formula.Steps {
			if s.ID != tc.step {
				continue
			}
			found = true
			if !strings.Contains(s.Description, proposeNotesHeading) {
				t.Errorf("%s: expected the closing step to name the %q section, got %q", tc.file, proposeNotesHeading, s.Description)
			}
			if !strings.Contains(s.Description, "does NOT edit the rig memory file") {
				t.Errorf("%s: expected the closing step to say the Builder does not edit the rig memory file, got %q", tc.file, s.Description)
			}
			if strings.Contains(s.Description, "add it to the rig's Builder memory file") || strings.Contains(s.Title, "update rig memory") {
				t.Errorf("%s: expected the closing step no longer to tell the Builder to write the rig memory, got %q / %q", tc.file, s.Title, s.Description)
			}
		}
		if !found {
			t.Errorf("%s: no step %q", tc.file, tc.step)
		}
	}
}

func TestClosingStepOfEachFormulaAsksForHowToCheckItBeforeTheRigMemoryNotes(t *testing.T) {
	for _, file := range []string{"tdd-feature.formula.json", "chore.formula.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "formulas", file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		var formula struct {
			Steps []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &formula); err != nil {
			t.Fatalf("decoding %s: %v", file, err)
		}
		found := false
		for _, s := range formula.Steps {
			if s.ID != "close" {
				continue
			}
			found = true
			at := strings.Index(s.Description, howToCheckHeading)
			if at < 0 {
				t.Errorf("%s: expected the closing step to name the %q section, got %q", file, howToCheckHeading, s.Description)
			}
			if !strings.Contains(s.Description, howToCheckInternal) {
				t.Errorf("%s: expected the closing step to give the line %q, got %q", file, howToCheckInternal, s.Description)
			}
			if !strings.Contains(s.Description, "at most six") {
				t.Errorf("%s: expected the closing step to cap the steps at six, got %q", file, s.Description)
			}
			if notes := strings.Index(s.Description, proposeNotesHeading); at >= 0 && notes >= 0 && at > notes {
				t.Errorf("%s: expected %q before %q, got %q", file, howToCheckHeading, proposeNotesHeading, s.Description)
			}
		}
		if !found {
			t.Errorf("%s: no step %q", file, "close")
		}
	}
}

func TestKickoffPromptsSayTheRigMemoryIsReadOnlyToTheSession(t *testing.T) {
	want := `memory of this rig is read-only to you: propose notes under "` + proposeNotesHeading + `" in your closing comment`
	prompts := map[string]string{
		"kickoff":        application.KickoffPrompt("builder", "mw-gq6.6", "/vault"),
		"rebase kickoff": application.RebaseKickoffPrompt("builder", "mw-gq6.50", "/vault", "origin/main"),
	}
	for name, prompt := range prompts {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the %s to hold %q, got %q", name, want, prompt)
		}
	}
}

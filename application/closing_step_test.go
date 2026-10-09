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
		"tester kickoff": application.TesterKickoffPrompt("builder", "mw-l.2", "/vault"),
	}
	for name, prompt := range prompts {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the %s to hold %q, got %q", name, want, prompt)
		}
	}
}

// A bug story is worked from a regression test that reproduces his report and
// is seen to fail first, and its closing comment names the bug's class and
// where else in the rig it can happen. The tdd-feature formula carries those
// words in two extra steps that bd pours only when the formula's kind is bug
// (mw next says so for a story of type bug or a title tagged [bug]); the steps
// every other story is poured with do not carry them.
const (
	bugCondition   = "{{kind}} == bug"
	otherCondition = "{{kind}} != bug"
)

type formulaStep struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Condition   string   `json:"condition"`
	DependsOn   []string `json:"depends_on"`
}

func tddFeatureSteps(t *testing.T) map[string]formulaStep {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "formulas", "tdd-feature.formula.json"))
	if err != nil {
		t.Fatalf("reading tdd-feature.formula.json: %v", err)
	}
	var formula struct {
		Steps []formulaStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &formula); err != nil {
		t.Fatalf("decoding tdd-feature.formula.json: %v", err)
	}
	steps := map[string]formulaStep{}
	for _, s := range formula.Steps {
		steps[s.ID] = s
	}
	return steps
}

func TestTheRedStepOfABugStoryWritesTheRegressionTestAndSeesItFailForHisReason(t *testing.T) {
	steps := tddFeatureSteps(t)
	bug, ok := steps["red-bug"]
	if !ok {
		t.Fatal("expected a red-bug step in tdd-feature")
	}
	if bug.Condition != bugCondition {
		t.Errorf("expected the red-bug step to be poured only for a bug, got condition %q", bug.Condition)
	}
	for _, want := range []string{"regression test", "from his report", "before any fix", "fail for the reason he saw", "cannot fail", "make the fake honest enough to fail"} {
		if !strings.Contains(bug.Description, want) {
			t.Errorf("expected the red-bug step to say %q, got %q", want, bug.Description)
		}
	}
	plain := steps["red"]
	if plain.Condition != otherCondition {
		t.Errorf("expected the plain red step to be poured only for what is not a bug, got condition %q", plain.Condition)
	}
	if strings.Contains(plain.Description, "regression test") {
		t.Errorf("expected the plain red step not to ask for a regression test, got %q", plain.Description)
	}
	for _, s := range []string{"green"} {
		if got := steps[s].DependsOn; len(got) != 2 || got[0] != "red" || got[1] != "red-bug" {
			t.Errorf("expected %s to wait on either red step, got %q", s, got)
		}
	}
	if got := steps["red-bug"].DependsOn; len(got) != 1 || got[0] != "understand" {
		t.Errorf("expected the red-bug step to wait on understand, got %q", got)
	}
}

func TestTheClosingStepOfABugStoryAsksForTheRegressionTestAndTheClassWithItsSweep(t *testing.T) {
	steps := tddFeatureSteps(t)
	bug, ok := steps["close-bug"]
	if !ok {
		t.Fatal("expected a close-bug step in tdd-feature")
	}
	if bug.Condition != bugCondition {
		t.Errorf("expected the close-bug step to be poured only for a bug, got condition %q", bug.Condition)
	}
	for _, want := range []string{"Regression test: <file:line>", "Class: <the kind of bug, one line>; sweep: <where else in the rig it can happen: fixed here, or filed as a [bug] with its id>"} {
		if !strings.Contains(bug.Description, want) {
			t.Errorf("expected the close-bug step to name %q, got %q", want, bug.Description)
		}
	}
	// The two lines come ahead of the Governor's section, which comes ahead of
	// the rig-memory notes; the rest of the closing step is the plain one's.
	regression, class := strings.Index(bug.Description, "Regression test:"), strings.Index(bug.Description, "Class:")
	check, notes := strings.Index(bug.Description, howToCheckHeading), strings.Index(bug.Description, proposeNotesHeading)
	if !(regression >= 0 && regression < class && class < check && check < notes) {
		t.Errorf("expected the Regression test and Class lines before %q before %q, got %q", howToCheckHeading, proposeNotesHeading, bug.Description)
	}
	plain := steps["close"]
	if plain.Condition != otherCondition {
		t.Errorf("expected the plain close step to be poured only for what is not a bug, got condition %q", plain.Condition)
	}
	for _, not := range []string{"Regression test:", "Class:", "sweep:"} {
		if strings.Contains(plain.Description, not) {
			t.Errorf("expected the plain close step not to ask for %q, got %q", not, plain.Description)
		}
	}
}

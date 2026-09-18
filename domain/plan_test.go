package domain_test

import (
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// testdata/walking-skeleton.json is a copy of the first plan the Mayor wrote
// (plans/0001-walking-skeleton.json in the vault). It is here so that the
// format mw file must accept is the format a test reads, not a paraphrase of
// it.
const realPlan = "testdata/walking-skeleton.json"

func TestTheMayorsOwnPlanIsRead(t *testing.T) {
	written, err := os.ReadFile(realPlan)
	if err != nil {
		t.Fatalf("reading %s: %v", realPlan, err)
	}
	plan, err := domain.ParsePlan(written)
	if err != nil {
		t.Fatalf("reading the plan: %v", err)
	}

	if plan.Epic.Title != "Walking skeleton: mw files, dispatches and closes out one story" {
		t.Errorf("expected the epic's title, got %q", plan.Epic.Title)
	}
	if plan.Epic.Success == "" || plan.Epic.Description == "" || plan.Epic.Priority != 1 {
		t.Errorf("expected the epic's description, success criteria and priority, got %+v", plan.Epic)
	}
	want := domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "vps",
	}
	if plan.Epic.Defaults != want {
		t.Errorf("expected the epic's default path %+v, got %+v", want, plan.Epic.Defaults)
	}
	if len(plan.Stories) != 10 {
		t.Fatalf("expected the plan's ten stories, got %d", len(plan.Stories))
	}

	if err := plan.Validate(); err != nil {
		t.Fatalf("expected the Mayor's own plan to be filable: %v", err)
	}

	// The story that overrides the default path is worked by the path it names.
	for _, story := range plan.Stories {
		if story.Key != "formulas" {
			continue
		}
		path, err := story.PathFrom(plan.Epic.Defaults)
		if err != nil {
			t.Fatalf("the formulas story has no path: %v", err)
		}
		if path.Model != domain.ModelSonnet || path.Formula != "chore" || path.Rig != "millwright" {
			t.Errorf("expected the overridden path, got %+v", path)
		}
		if story.Estimate != 30 || story.Acceptance == "" {
			t.Errorf("expected the story's estimate and acceptance criteria, got %+v", story)
		}
	}
}

func TestAPlanIsFiledWithNoStoryBeforeWhatItWaitsOn(t *testing.T) {
	written, err := os.ReadFile(realPlan)
	if err != nil {
		t.Fatalf("reading %s: %v", realPlan, err)
	}
	plan, err := domain.ParsePlan(written)
	if err != nil {
		t.Fatalf("reading the plan: %v", err)
	}

	order, err := plan.Order()
	if err != nil {
		t.Fatalf("ordering the plan: %v", err)
	}
	if len(order) != len(plan.Stories) {
		t.Fatalf("expected all %d stories in the order, got %d", len(plan.Stories), len(order))
	}

	placed := map[string]bool{}
	for _, story := range order {
		for _, need := range story.Needs {
			if !placed[need] {
				t.Fatalf("%s is filed before %s, which it waits on", story.Key, need)
			}
		}
		placed[story.Key] = true
	}
}

func TestAPlanSaysEveryReasonItCannotBeFiled(t *testing.T) {
	plan := domain.Plan{
		Epic: domain.PlanEpic{Defaults: domain.Path{Rig: "millwright", Harness: domain.HarnessClaude, Model: domain.ModelOpus, Effort: domain.EffortHigh}},
		Stories: []domain.PlanStory{
			{Key: "module", Title: "Go module"},
			{Key: "module", Title: "Go module again", Acceptance: "it passes"},
			{Key: "gateway", Title: "Gateway", Acceptance: "it passes", Needs: []string{"runner"}},
		},
	}

	err := plan.Validate()
	if err == nil {
		t.Fatal("expected a plan with this much wrong with it to be refused")
	}
	var refused *domain.PlanRefused
	if !asPlanRefused(err, &refused) {
		t.Fatalf("expected the refusal to carry its reasons, got %T", err)
	}
	for _, want := range []string{
		"the epic has no title",
		"story module has no acceptance criteria",
		"two stories share the key module",
		"branch is required",
		"story gateway waits on runner, which no story in this plan is",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, err)
		}
	}
}

func TestStoriesThatWaitOnEachOtherAreRefused(t *testing.T) {
	defaults := domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelOpus, Effort: domain.EffortHigh}
	plan := domain.Plan{
		Epic: domain.PlanEpic{Title: "Loop", Defaults: defaults},
		Stories: []domain.PlanStory{
			{Key: "a", Title: "A", Acceptance: "it passes", Needs: []string{"c"}},
			{Key: "b", Title: "B", Acceptance: "it passes"},
			{Key: "c", Title: "C", Acceptance: "it passes", Needs: []string{"a"}},
		},
	}

	err := plan.Validate()
	if err == nil {
		t.Fatal("expected stories that wait on each other to be refused")
	}
	if !strings.Contains(err.Error(), "these stories wait on each other: a needs c needs a") {
		t.Errorf("expected the refusal to name the loop, got:\n%s", err)
	}
	if _, err := plan.Order(); err == nil {
		t.Fatal("expected stories that wait on each other to have no order")
	}

	// A story that waits on itself is a loop of one, and is named as such.
	plan.Stories = []domain.PlanStory{{Key: "a", Title: "A", Acceptance: "it passes", Needs: []string{"a"}}}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "story a waits on itself") {
		t.Errorf("expected a story that waits on itself to be refused, got %v", err)
	}
}

func TestAPlanThatIsNotAPlanSaysSo(t *testing.T) {
	if _, err := domain.ParsePlan([]byte("{")); err == nil {
		t.Fatal("expected half a JSON document not to be a plan")
	}
	// A key the format does not know is a refusal, not a silence.
	written := []byte(`{"epic":{"title":"E","defaults":{"brnach":"main"}},"stories":[]}`)
	if _, err := domain.ParsePlan(written); err == nil {
		t.Fatal("expected a misspelt path field to be refused")
	}
}

func TestAPathReadsAsOneLine(t *testing.T) {
	full := domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelSonnet, Effort: domain.EffortHigh, Formula: "chore", Host: "vps",
	}
	if got, want := full.Summary(), "millwright/main · claude/sonnet/high · chore · vps"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	// A path that sets nothing says so rather than printing separators.
	if got := (domain.Path{}).Summary(); got != "no path" {
		t.Errorf("expected an empty path to read as no path, got %q", got)
	}
	if got, want := (domain.Path{Rig: "millwright", Model: domain.ModelOpus}).Summary(), "millwright · opus"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// asPlanRefused is errors.As, spelled out so that the domain's test stays as
// plain as the domain.
func asPlanRefused(err error, target **domain.PlanRefused) bool {
	refused, ok := err.(*domain.PlanRefused)
	if ok {
		*target = refused
	}
	return ok
}

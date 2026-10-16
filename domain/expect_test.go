package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestTheLinesBeginningAWordAreReadAfterABulletOrNumber(t *testing.T) {
	acceptance := "1. It works.\n2. Sweep: every caller\n- sweep:  all of them \n3) Expect: refusals down\nNo Sweep: here"
	got := domain.LinesBeginning(acceptance, "Sweep")
	if len(got) != 2 || got[0] != "every caller" || got[1] != "all of them" {
		t.Errorf("Sweep lines %q, want the two that begin with it", got)
	}
	if got := domain.LinesBeginning(acceptance, "Expect"); len(got) != 1 || got[0] != "refusals down" {
		t.Errorf("Expect lines %q, want one", got)
	}
	if got := domain.LinesBeginning(acceptance, "Fixes"); len(got) != 0 {
		t.Errorf("Fixes lines %q, want none", got)
	}
}

func TestParseExpectReadsTheMetricAndTheDirection(t *testing.T) {
	for text, want := range map[string]domain.Expect{
		"1. It works.\n2. Expect: refusals down":                        {Metric: "refusals", Direction: "down"},
		"Expect: attempts/landing down, because the formula is shorter": {Metric: "attempts/landing", Direction: "down"},
		"- expect: $/landing UP":                                        {Metric: "$/landing", Direction: "up"},
		"3) Expect: dispatch-to-landing down.":                          {Metric: "dispatch-to-landing", Direction: "down"},
		"Expect: tokens/landing down":                                   {Metric: "tokens/landing", Direction: "down"},
		"Expect: bugs/landing down":                                     {Metric: "bugs/landing", Direction: "down"},
		"Expect: none, because this is a doc":                           {Metric: domain.ExpectNone},
	} {
		got, err := domain.ParseExpect(text)
		if err != nil || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", text, got, err, want)
		}
	}
	if _, err := domain.ParseExpect("1. It works\nSweep: none, because it is the tool"); !errors.Is(err, domain.ErrNoExpect) {
		t.Errorf("acceptance with no Expect line: got %v, want ErrNoExpect", err)
	}
	for _, bad := range []string{"Expect: speed up", "Expect: refusals", "Expect: refusals sideways", "Expect:", "Expect: none", "Expect: none, because"} {
		if _, err := domain.ParseExpect(bad); err == nil || errors.Is(err, domain.ErrNoExpect) {
			t.Errorf("%q was accepted or taken for no line: %v", bad, err)
		}
	}
}

// expectPlan is a filable plan of one story in rig, with acceptance.
func expectPlan(rig, acceptance string) domain.Plan {
	return domain.Plan{
		Epic: domain.PlanEpic{Title: "Factory work", Defaults: domain.Path{Rig: rig, Branch: "main",
			Harness: domain.HarnessClaude, Model: domain.ModelOpus, Effort: domain.EffortHigh}},
		Stories: []domain.PlanStory{{Key: "judge", Title: "Judge the changes", Acceptance: acceptance}},
	}
}

func TestAMillwrightStoryWithNoReadableExpectIsNotFiled(t *testing.T) {
	for _, acceptance := range []string{
		"1. It works.",
		"1. It works.\n2. Expect: speed up",
		"1. It works.\n2. Expect: none",
	} {
		err := expectPlan("millwright", acceptance).Validate()
		if err == nil {
			t.Errorf("%q: a millwright story with no readable Expect was filable", acceptance)
			continue
		}
		for _, want := range []string{"story judge", "Expect: <metric> up", "Expect: none, because <why>"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%q: the refusal does not say %q:\n%s", acceptance, want, err)
			}
		}
	}
	// A story whose own path names the rig is held to it as well.
	plan := expectPlan("lampas", "1. It works.")
	plan.Stories[0].Overrides.Rig = "millwright"
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "Expect:") {
		t.Errorf("a story moved to millwright by its own path was filable with no Expect: %v", err)
	}
}

func TestAMillwrightStoryWithAnExpectAndAnotherRigsStoryWithoutOneAreFiled(t *testing.T) {
	for _, plan := range []domain.Plan{
		expectPlan("millwright", "1. It works.\n2. Expect: refusals down"),
		expectPlan("millwright", "1. It works.\nExpect: none, because it is a doc"),
		expectPlan("lampas", "1. It works."),
	} {
		if err := plan.Validate(); err != nil {
			t.Errorf("%+v was refused: %v", plan.Stories[0], err)
		}
	}
}

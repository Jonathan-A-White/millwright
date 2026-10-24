package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func gristAcceptancePlan(acceptance string) domain.Plan {
	plan := twoStoryPlan()
	plan.Stories = plan.Stories[:1]
	plan.Stories[0].Acceptance = acceptance
	return plan
}

func gristFile(out, warn *strings.Builder) application.File {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	return application.File{
		Tracker:   tracker,
		GristApps: map[string]string{"lampas": "/rigs/lampas", "trade-tracker": "/rigs/tt"},
		Out:       out,
		Err:       warn,
	}
}

func TestAnAcceptanceThatEvalsAnAppNotInGristAppsIsRefusedNamingTheApps(t *testing.T) {
	var out, warn strings.Builder
	err := gristFile(&out, &warn).Check(context.Background(),
		gristAcceptancePlan("mw grist eval night-stack tutor --example hello --ref /wt passes\nExpect: none, because a test plan"))
	var refused *domain.PlanRefused
	if !errors.As(err, &refused) {
		t.Fatalf("expected the plan refused, got %v", err)
	}
	text := strings.Join(refused.Reasons, "\n")
	for _, want := range []string{"module", "night-stack", "[grist-apps]", "lampas", "trade-tracker"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected the refusal to name %q, got %q", want, text)
		}
	}
	t.Log(text)
}

func TestAnAcceptanceThatSmokesAnAppNotInGristAppsIsRefused(t *testing.T) {
	var out, warn strings.Builder
	err := gristFile(&out, &warn).Check(context.Background(),
		gristAcceptancePlan("`mw grist smoke night-stack --ref /wt` prints no warning\nExpect: none, because a test plan"))
	if err == nil || !strings.Contains(err.Error(), "night-stack") {
		t.Fatalf("expected the plan refused for night-stack, got %v", err)
	}
}

func TestAnAcceptanceOfAKnownAppIsNotRefused(t *testing.T) {
	var out, warn strings.Builder
	err := gristFile(&out, &warn).Check(context.Background(),
		gristAcceptancePlan("mw grist eval lampas tutor --example hello --ref /wt passes\nExpect: none, because a test plan"))
	if err != nil {
		t.Fatalf("expected the plan to pass, got %v", err)
	}
	if warn.Len() != 0 {
		t.Errorf("expected no warning for eval, got %q", warn.String())
	}
}

func TestAHostWithNoGristAppsRefusesNoGristAcceptance(t *testing.T) {
	var out, warn strings.Builder
	f := gristFile(&out, &warn)
	f.GristApps = nil
	if err := f.Check(context.Background(),
		gristAcceptancePlan("mw grist eval night-stack tutor\nExpect: none, because a test plan")); err != nil {
		t.Fatalf("expected nothing checked without the table, got %v", err)
	}
}

func TestAnAcceptanceThatSmokesWithNoRefIsWarnedOf(t *testing.T) {
	var out, warn strings.Builder
	err := gristFile(&out, &warn).Check(context.Background(),
		gristAcceptancePlan("mw grist smoke lampas passes\nExpect: none, because a test plan"))
	if err != nil {
		t.Fatalf("expected a warning and not a refusal, got %v", err)
	}
	for _, want := range []string{"module", "smoke reads main", "mw grist eval <app> <kind> --example <name> --ref <worktree>"} {
		if !strings.Contains(warn.String(), want) {
			t.Errorf("expected the warning to say %q, got %q", want, warn.String())
		}
	}
	t.Log(warn.String())
}

func TestAnAcceptanceThatSmokesWithARefIsNotWarnedOf(t *testing.T) {
	var out, warn strings.Builder
	if err := gristFile(&out, &warn).Check(context.Background(),
		gristAcceptancePlan("mw grist smoke lampas --ref /wt prints no warning\nExpect: none, because a test plan")); err != nil {
		t.Fatal(err)
	}
	if warn.Len() != 0 {
		t.Errorf("expected no warning, got %q", warn.String())
	}
}

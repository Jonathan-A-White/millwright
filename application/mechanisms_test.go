package application_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

const acrossMechanisms = "1. A new setting next.foo is read, default 10.\n2. A new component, the Foo screen, shows it.\n3. The old bar command is removed.\nExpect: refusals down"
const withinAMechanism = "1. A new setting next.foo is read, default 10.\n2. A second new setting, next.bar, is read too.\nExpect: refusals down"

func pathSetSized(t *testing.T, acceptance string, set ...string) (out, warn string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("E", domain.Path{Rig: "lampas", Branch: "main", Harness: "claude", Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "vps"})
	tracker.AddStory("E", domain.Story{ID: "B", Title: "b"})
	if err := tracker.SetAcceptance("B", acceptance); err != nil {
		t.Fatal(err)
	}
	var o, w bytes.Buffer
	if _, err := (application.PathSet{Tracker: tracker, Out: &o, Err: &w}).Run(t.Context(), "B", "", set); err != nil {
		t.Fatal(err)
	}
	return o.String(), w.String()
}

func TestPathSetWarnsOfALargeStoryAcrossMechanisms(t *testing.T) {
	_, warn := pathSetSized(t, acrossMechanisms, "size=L")
	for _, want := range []string{"WARN:", "3 mechanisms", "a new setting (1)", "a new component (2)", "a removal (3)"} {
		if !strings.Contains(warn, want) {
			t.Errorf("stderr lacks %q:\n%s", want, warn)
		}
	}
}

func TestPathSetSaysNothingOfALargeStoryWithinOneMechanism(t *testing.T) {
	out, warn := pathSetSized(t, withinAMechanism, "size=XL")
	if strings.Contains(out+warn, "mechanisms") {
		t.Errorf("warned of a story of one mechanism:\n%s%s", out, warn)
	}
}

func TestPathSetSaysNothingOfASmallerStoryAcrossMechanisms(t *testing.T) {
	out, warn := pathSetSized(t, acrossMechanisms, "size=M")
	if strings.Contains(out+warn, "mechanisms") {
		t.Errorf("warned of an M story:\n%s%s", out, warn)
	}
}

func checkWarnings(t *testing.T, size domain.Size, acceptance string) string {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Stories[0].Overrides.Size = size
	plan.Stories[0].Acceptance = acceptance
	var out, warn bytes.Buffer
	if err := (application.File{Tracker: tracker, Out: &out, Err: &warn}).Check(t.Context(), plan); err != nil {
		t.Fatalf("checking the plan: %v", err)
	}
	return warn.String()
}

func TestFileCheckWarnsOfOneLargeStoryOverAndNotOneUnder(t *testing.T) {
	over := checkWarnings(t, domain.SizeL, acrossMechanisms+"\nSweep: none.")
	if !strings.Contains(over, "3 mechanisms") || !strings.Contains(over, "a removal (3)") {
		t.Errorf("expected the story over to be warned of, got:\n%s", over)
	}
	if under := checkWarnings(t, domain.SizeL, withinAMechanism); strings.Contains(under, "mechanisms") {
		t.Errorf("expected the story under to be passed, got:\n%s", under)
	}
}

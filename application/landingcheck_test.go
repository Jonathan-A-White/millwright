package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// A criterion that says it is the landing check from his use, or not the
// Builder's, is filed into the Builder's list and refused after a full run
// (mw-gq6.1567): mw path set warns of it, naming a VERIFY AFTER LANDING line.
func TestPathSetWarnsOfACriterionThatIsTheLandingCheckFromHisUse(t *testing.T) {
	for name, acceptance := range map[string]string{
		"from his use":        "1. it builds.\n2. (The landing check from his use) one real run puts a text on his phone",
		"from his use, cased": "1. it builds.\n2. The check From His Use shows the banner",
		"not the Builder's":   "1. it builds.\n2. One real run (not the Builder's) sends the mail",
		"a curly apostrophe":  "1. it builds.\n2. One real run (not the Builder’s) sends the mail",
	} {
		tracker, rules := aStoryWithAcceptance(t, acceptance)
		var errOut strings.Builder
		if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
			t.Fatalf("%s: nothing is refused for it, got %v", name, err)
		}
		got := errOut.String()
		if strings.Count(got, "WARN") != 1 || !strings.Contains(got, "mw-gq6.2") || !strings.Contains(got, "criterion 2") ||
			!strings.Contains(got, "VERIFY AFTER LANDING") {
			t.Errorf("%s: expected one warning naming the story, criterion 2 and a VERIFY AFTER LANDING line; got %q", name, got)
		}
	}
}

func TestPathSetSaysNothingOfAVerifyAfterLandingLineInTheDescription(t *testing.T) {
	tracker, rules := aStoryWithAcceptance(t, "1. it builds.\n2. go test ./application passes")
	var errOut strings.Builder
	if _, err := (application.PathSet{Tracker: tracker, Rules: rules, Err: &errOut}).Run(context.Background(), "mw-gq6.2", "mw-gq6.1", nil); err != nil {
		t.Fatal(err)
	}
	if errOut.Len() != 0 {
		t.Errorf("expected no warning, got %q", errOut.String())
	}
}

// mw file warns of it too, in Check and in Run, as it warns of the live URL.
func TestFileWarnsOfACriterionThatIsTheLandingCheckFromHisUse(t *testing.T) {
	acceptance := "The landing check from his use puts a text on his phone\nExpect: none, because a test plan"
	var checkOut, checkWarn, runOut, runWarn strings.Builder
	if err := gristFile(&checkOut, &checkWarn).Check(context.Background(), gristAcceptancePlan(acceptance)); err != nil {
		t.Fatalf("check: expected a warning and not a refusal, got %v", err)
	}
	if _, err := gristFile(&runOut, &runWarn).Run(context.Background(), gristAcceptancePlan(acceptance)); err != nil {
		t.Fatalf("file: expected a warning and not a refusal, got %v", err)
	}
	for name, warn := range map[string]string{"mw file --check": checkWarn.String(), "mw file": runWarn.String()} {
		for _, want := range []string{"WARN:", "criterion 1", "VERIFY AFTER LANDING"} {
			if !strings.Contains(warn, want) {
				t.Errorf("%s: expected the warning to say %q, got %q", name, want, warn)
			}
		}
	}
}

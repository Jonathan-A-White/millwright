package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1694: an approved plan releases each story it filed except one whose
// acceptance holds a criterion that needs its own landing, which stays held and
// is named, as mw release and mw release-paced leave it.
func TestFileApproveRefusesANotRunnableCriterion(t *testing.T) {
	plan := twoStoryPlan()
	plan.Stories[0].Acceptance = "curl -s https://postern.allmymind.org/ prints ok\nExpect: none, because a test plan"
	plan.Stories[1].Needs = nil
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	var out strings.Builder

	filed, err := application.File{
		Tracker: tracker,
		Out:     &out,
		Approve: func(context.Context, application.FiledPlan) (bool, error) { return true, nil },
	}.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("filing the plan: %v", err)
	}

	live, runnable := filed.Stories[0].ID, filed.Stories[1].ID
	if got := statusOf(t, tracker, live); got != application.StatusHeld {
		t.Errorf("expected %s left held, got %s", live, got)
	}
	if got := statusOf(t, tracker, runnable); got == application.StatusHeld {
		t.Errorf("expected %s released, it is %s", runnable, got)
	}
	for _, said := range []string{live, "criterion 1", "Verify strings:", "skipped", "mw release --force"} {
		if !strings.Contains(out.String(), said) {
			t.Errorf("expected the output to say %q, got:\n%s", said, out.String())
		}
	}
	if strings.Contains(out.String(), "Released all 2 stories") {
		t.Errorf("expected the report not to say every story was released, got:\n%s", out.String())
	}
}

package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// openStepsRows files five open-steps refusals on 2026-10-18 and a landing;
// the stories named hands keep a hands step.
func openStepsRows(t *testing.T, tracker *apptest.FakeTracker, files *vault.Vault, hands ...string) {
	t.Helper()
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	ids := []string{"os-1", "os-2", "os-3", "os-4", "os-5"}
	for _, id := range ids {
		tracker.AddStory("", domain.Story{ID: id, Title: "Story " + id, Overrides: domain.Path{Rig: "lampas", Host: "laptop", Model: "sonnet"}})
		line := application.LedgerLine{When: at, StoryID: id, Title: "The story " + id,
			Outcome: application.NotLanded(application.ReasonOpenSteps, "2 formula step(s) are still open"),
			Path:    domain.Path{Model: "sonnet", Effort: "high"}}.String()
		if err := files.AppendToLedger(context.Background(), "builder", line); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range hands {
		raw, err := json.Marshal([]application.HandsStepRecord{{HandsStep: domain.HandsStep{ID: "s1", Host: "vps", As: "root", Run: "systemctl restart x"}, AddedAt: "2026-10-17T09:00:00Z"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetNote(context.Background(), application.HandsStepsKey(id), string(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

func runOpenStepsReport(t *testing.T, tracker *apptest.FakeTracker, files *vault.Vault, hands application.ReportHands) (application.FactoryReportResult, string) {
	t.Helper()
	var out strings.Builder
	result, err := application.FactoryReport{
		Tracker: tracker, Files: files, Seat: "builder", Benchmarks: &apptest.FakeBenchmarks{}, Hands: hands,
		Now: func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }, Out: &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("mw report failed: %v", err)
	}
	return result, out.String()
}

func TestReportOpenStepsSplitPrintsBothCountsAndTheirSumIsTheOldCount(t *testing.T) {
	tracker, files := apptest.NewFakeTracker(), vault.New(t.TempDir())
	openStepsRows(t, tracker, files, "os-2", "os-4")

	result, printed := runOpenStepsReport(t, tracker, files, tracker)

	line := "open-steps 5: left open 3 · hands waiting on him 2"
	if !strings.Contains(printed, line) {
		t.Errorf("the report does not say %q:\n%s", line, printed)
	}
	for _, text := range strings.Split(printed, "\n") {
		if len([]rune(text)) > application.ReportWidth {
			t.Errorf("a line is wider than %d columns: %q", application.ReportWidth, text)
		}
	}
	whole := result.Whole.Quality
	if whole.OpenSteps == nil {
		t.Fatalf("the quality has no open-steps split: %+v", whole)
	}
	if got := whole.OpenSteps.Builder + whole.OpenSteps.Hands; got != 5 || whole.OpenSteps.Hands != 2 {
		t.Errorf("split %+v, want 3 + 2 = 5", whole.OpenSteps)
	}
	for _, r := range whole.Reasons {
		if r.Reason == "open-steps" && r.Count != 5 {
			t.Errorf("open-steps counted %d, want the old 5", r.Count)
		}
	}
}

func TestReportOpenStepsSplitWithNoHandsReaderKeepsTheOldLine(t *testing.T) {
	tracker, files := apptest.NewFakeTracker(), vault.New(t.TempDir())
	openStepsRows(t, tracker, files, "os-2")

	result, printed := runOpenStepsReport(t, tracker, files, nil)

	if !strings.Contains(printed, "open-steps 5\n") || strings.Contains(printed, "hands waiting") {
		t.Errorf("without a hands reader the line should be as before:\n%s", printed)
	}
	if result.Whole.Quality.OpenSteps != nil {
		t.Errorf("a split was made with no hands reader: %+v", result.Whole.Quality.OpenSteps)
	}
}

// pourTwice pours a formula for the story twice, as a retry does, points the
// story at the second pouring, and returns both.
func pourTwice(t *testing.T, tracker *apptest.FakeTracker, story string) (earlier, current application.Molecule) {
	t.Helper()
	ctx := context.Background()
	tracker.AddFormula("tdd-feature", application.FormulaStep{Title: "Understand"}, application.FormulaStep{Title: "Commit"})
	var err error
	if earlier, err = tracker.PourFormula(ctx, "tdd-feature", story, "The story", false); err != nil {
		t.Fatal(err)
	}
	if current, err = tracker.PourFormula(ctx, "tdd-feature", story, "The story", false); err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(ctx, story, map[string]string{application.MoleculeField: current.RootID}); err != nil {
		t.Fatal(err)
	}
	return earlier, current
}

func markGovernors(t *testing.T, tracker *apptest.FakeTracker, story, step string) {
	t.Helper()
	raw, err := json.Marshal(application.GovernorsStep{Story: story, Step: step, Reason: "only he can", At: "2026-10-17T09:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetNote(context.Background(), application.GovernorsStepKey(story, step), string(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestAMarkOfAnEarlierAttemptsStepIsNotAHandsWait(t *testing.T) {
	tracker, files := apptest.NewFakeTracker(), vault.New(t.TempDir())
	openStepsRows(t, tracker, files)
	// os-2's earlier attempt marked a step the Governor's; its retry poured a
	// fresh formula whose steps the Builder left open. os-4 marked a step of its
	// current formula.
	earlier, _ := pourTwice(t, tracker, "os-2")
	markGovernors(t, tracker, "os-2", earlier.Steps[0].ID)
	_, current := pourTwice(t, tracker, "os-4")
	markGovernors(t, tracker, "os-4", current.Steps[0].ID)

	result, printed := runOpenStepsReport(t, tracker, files, tracker)

	split := result.Whole.Quality.OpenSteps
	if split == nil || split.Hands != 1 || split.Builder != 4 {
		t.Errorf("split %+v, want left open 4 and hands waiting 1 (a stale mark is no wait):\n%s", split, printed)
	}
}

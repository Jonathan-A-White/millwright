package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// wrongRigHost is this host's two checkouts: millwright holds Go, postern
// holds the todo board's TypeScript.
func wrongRigHost() (map[string]string, *apptest.FakeRigFiles) {
	files := apptest.NewFakeRigFiles()
	files.Add("/rigs/millwright", "application/status.go", "cmd/mw/main.go")
	files.Add("/rigs/postern", "src/todo/TodoBoard.tsx", "src/todo/lists.ts")
	return map[string]string{"millwright": "/rigs/millwright", "postern": "/rigs/postern"}, files
}

func wrongRigPlan(files string) domain.Plan {
	plan := twoStoryPlan()
	plan.Epic.Defaults.Host = "desktop"
	plan.Stories = []domain.PlanStory{{Key: "board", Title: "Todo board", Acceptance: "make test passes", Files: files}}
	return plan
}

const wrongRigLine = "none of its files exist in millwright: wrong rig?"

func TestFilingAStoryWhoseFilesExistOnlyInAnotherRigWarns(t *testing.T) {
	rigs, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	out := &strings.Builder{}

	filed, err := application.File{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: out}.
		Run(context.Background(), wrongRigPlan("src/todo/TodoBoard.tsx, src/todo/lists.ts"))
	if err != nil {
		t.Fatalf("filing: %v", err)
	}
	want := filed.Stories[0].ID + ": " + wrongRigLine
	if !strings.Contains(out.String(), want) {
		t.Errorf("expected %q, got:\n%s", want, out.String())
	}
}

func TestFilingAStoryWithOneFileInItsRigDoesNotWarn(t *testing.T) {
	rigs, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	out := &strings.Builder{}

	_, err := application.File{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: out}.
		Run(context.Background(), wrongRigPlan("application/status.go, application/brand_new.go"))
	if err != nil {
		t.Fatalf("filing: %v", err)
	}
	if strings.Contains(out.String(), "wrong rig") {
		t.Errorf("expected no warning, got:\n%s", out.String())
	}
}

func TestFilingAStoryWithNoFilesKeyIsUnchanged(t *testing.T) {
	rigs, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	out := &strings.Builder{}

	if _, err := (application.File{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: out}).
		Run(context.Background(), wrongRigPlan("")); err != nil {
		t.Fatalf("filing: %v", err)
	}
	if strings.Contains(out.String(), "wrong rig") {
		t.Errorf("expected no warning, got:\n%s", out.String())
	}
}

func TestAGlobOfAnotherRigIsNotFoundInThisOne(t *testing.T) {
	rigs, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	out := &strings.Builder{}

	if _, err := (application.File{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: out}).
		Run(context.Background(), wrongRigPlan("src/todo/*.tsx")); err != nil {
		t.Fatalf("filing: %v", err)
	}
	if !strings.Contains(out.String(), wrongRigLine) {
		t.Errorf("expected the warning, got:\n%s", out.String())
	}
}

func TestAHeldStoryInTheWrongRigWarnsWhenReleasedAndIsReleasedAll(t *testing.T) {
	rigs, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	filed, err := application.File{Tracker: tracker}.Run(context.Background(), wrongRigPlan("src/todo/lists.ts"))
	if err != nil {
		t.Fatalf("filing: %v", err)
	}
	out := &strings.Builder{}

	found, err := application.Release{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: out}.Run(context.Background(), filed.EpicID)
	if err != nil {
		t.Fatalf("releasing: %v", err)
	}
	if !found.Released {
		t.Error("expected the release to go ahead: the warning refuses nothing")
	}
	if want := filed.Stories[0].ID + ": " + wrongRigLine; !strings.Contains(out.String(), want) {
		t.Errorf("expected %q, got:\n%s", want, out.String())
	}

	one := &strings.Builder{}
	filed, err = application.File{Tracker: tracker}.Run(context.Background(), wrongRigPlan("src/todo/lists.ts"))
	if err != nil {
		t.Fatalf("filing: %v", err)
	}
	id := filed.Stories[0].ID
	if _, err := (application.Release{Tracker: tracker, Rigs: rigs, RigFiles: files, Out: one}).Run(context.Background(), id); err != nil {
		t.Fatalf("releasing %s: %v", id, err)
	}
	if want := id + ": " + wrongRigLine; !strings.Contains(one.String(), want) {
		t.Errorf("expected %q, got:\n%s", want, one.String())
	}
}

func TestARigNotCheckedOutHereIsNotCalledWrong(t *testing.T) {
	_, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	out := &strings.Builder{}

	if _, err := (application.File{Tracker: tracker, Rigs: map[string]string{"postern": "/rigs/postern"}, RigFiles: files, Out: out}).
		Run(context.Background(), wrongRigPlan("src/todo/lists.ts")); err != nil {
		t.Fatalf("filing: %v", err)
	}
	if strings.Contains(out.String(), "wrong rig") {
		t.Errorf("a rig this host does not have cannot be judged, got:\n%s", out.String())
	}
}

func TestStatusNamesTheOpenAndHeldStoriesFiledIntoTheWrongRig(t *testing.T) {
	rigs, files := wrongRigHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("ep", domain.Path{Rig: "millwright", Branch: "main", Host: "desktop"})
	for _, s := range []struct{ id, files string }{
		{"ep.1", "src/todo/TodoBoard.tsx"},
		{"ep.2", "application/status.go"},
		{"ep.3", ""},
	} {
		story := domain.Story{ID: s.id, Title: s.id}
		if s.files != "" {
			if err := story.Overrides.Set("files", s.files); err != nil {
				t.Fatal(err)
			}
		}
		tracker.AddStory("ep", story)
	}

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder", Rigs: rigs, RigFiles: files,
		Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if len(report.WrongRig) != 1 || report.WrongRig[0].ID != "ep.1" || report.WrongRig[0].Rig != "millwright" {
		t.Fatalf("expected only ep.1 to be named, got %+v", report.WrongRig)
	}
	text := report.String()
	if want := "ep.1: " + wrongRigLine; !strings.Contains(text, want) {
		t.Errorf("expected %q in:\n%s", want, text)
	}
	if strings.Contains(text, "ep.2: none") || strings.Contains(text, "ep.3: none") {
		t.Errorf("named a story that is not in the wrong rig:\n%s", text)
	}
}

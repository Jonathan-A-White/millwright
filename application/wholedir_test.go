package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// lampasHost is this host's checkout of lampas: a src of more files than a
// files key may name at once, a settings directory of three, and a features
// directory of two, an e2e directory of 25 specs and a json, and three steps.
func lampasHost() (map[string]string, *apptest.FakeRigFiles) {
	files := apptest.NewFakeRigFiles()
	for n := 0; n <= application.MostFilesInADirectory; n++ {
		files.Add("/rigs/lampas", fmt.Sprintf("src/File%d.tsx", n))
	}
	for n := 0; n <= application.MostFilesInADirectory+4; n++ {
		files.Add("/rigs/lampas", fmt.Sprintf("tests/e2e/spec%d.ts", n))
	}
	files.Add("/rigs/lampas", "tests/e2e/data.json", "features/steps/a.ts", "features/steps/b.ts", "features/steps/c.tsx")
	files.Add("/rigs/lampas", "src/settings/a.ts", "src/settings/b.ts", "src/settings/c.ts",
		"features/ask-composer.feature", "features/talk.feature")
	return map[string]string{"lampas": "/rigs/lampas"}, files
}

// aLampasStory is an open story on lampas with no files key yet.
func aLampasStory(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-5r3p30", domain.Path{Rig: "lampas", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelOpus, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: "cloud1"})
	tracker.AddStory("mw-5r3p30", domain.Story{ID: "mw-5r3p30.258", Title: "Talk sends twice"})
	return tracker
}

func setFiles(t *testing.T, rigs map[string]string, files application.RigFiles, key string) (*apptest.FakeTracker, error) {
	t.Helper()
	tracker := aLampasStory(t)
	_, err := application.PathSet{Tracker: tracker, Rigs: rigs, RigFiles: files}.
		Run(context.Background(), "mw-5r3p30.258", "", []string{"files=" + key})
	return tracker, err
}

func TestPathSetRefusesABareDirectoryOfTooManyFiles(t *testing.T) {
	rigs, files := lampasHost()
	tracker, err := setFiles(t, rigs, files, "src/Talk.tsx,src/,features/ask-composer.feature")
	if err == nil {
		t.Fatal("expected files=...src/... to be refused")
	}
	for _, said := range []string{
		"src/", fmt.Sprintf("more than %d files", application.MostFilesInADirectory),
		"every running story", "name the files the story will change", "nothing was written",
	} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-5r3p30.258"); detail.Story.Overrides.Files != "" {
		t.Errorf("expected nothing written, got files=%q", detail.Story.Overrides.Files)
	}
}

func TestPathSetRefusesABareDirectoryWhereTheRigIsNotCheckedOut(t *testing.T) {
	_, err := setFiles(t, nil, nil, "src/")
	if err == nil {
		t.Fatal("expected files=src/ to be refused with no checkout to count it in")
	}
	for _, said := range []string{"src/", "a whole directory", "not checked out here", "name the files the story will change"} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
}

func TestPathSetRefusesEveryWayOfNamingACrowdedDirectory(t *testing.T) {
	rigs, files := lampasHost()
	for _, key := range []string{"src", "./src/", "src/*", "src/**", "**"} {
		if _, err := setFiles(t, rigs, files, key); err == nil {
			t.Errorf("expected files=%s to be refused", key)
		}
	}
}

func TestPathSetTakesFilesGlobsAndSmallOrNewDirectories(t *testing.T) {
	rigs, files := lampasHost()
	for _, key := range []string{
		"src/Talk.tsx,src/File3.tsx,features/ask-composer.feature",
		"src/settings/*.ts",
		"features/steps/*.ts,features/steps/*.tsx",
		"tests/e2e/*.json",
		"tests/e2e/spec1?.ts",
		"src/settings/",
		"src/settings",
		"features/",
		"contrib/stt-bakeoff/",
		"src/hardSounds/**",
	} {
		tracker, err := setFiles(t, rigs, files, key)
		if err != nil {
			t.Errorf("expected files=%s to be taken, got %v", key, err)
			continue
		}
		if detail, _ := tracker.ShowStory(context.Background(), "mw-5r3p30.258"); detail.Story.Overrides.Files != key {
			t.Errorf("expected files=%s written, got %q", key, detail.Story.Overrides.Files)
		}
	}
}

func TestFileRefusesAStoryWhoseFilesKeyNamesACrowdedDirectory(t *testing.T) {
	rigs, files := lampasHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Epic.Defaults.Rig = "lampas"
	plan.Stories = []domain.PlanStory{{Key: "talk", Title: "Talk sends twice", Acceptance: "make test passes\nExpect: none, because a test plan",
		Files: "src/Talk.tsx,src/,features/ask-composer.feature"}}

	_, err := application.File{Tracker: tracker, Rigs: rigs, RigFiles: files}.Run(context.Background(), plan)
	if err == nil {
		t.Fatal("expected the plan to be refused")
	}
	for _, said := range []string{"talk", "src/", "every running story", "name the files the story will change"} {
		if !strings.Contains(err.Error(), said) {
			t.Errorf("expected the refusal to say %q, got %q", said, err)
		}
	}
	if epics := tracker.Epics(); len(epics) != 0 {
		t.Errorf("expected nothing filed, got %v", epics)
	}
}

func TestFileRefusesABareDirectoryWithNoCheckoutToCountIt(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Stories = []domain.PlanStory{{Key: "talk", Title: "Talk sends twice", Acceptance: "make test passes\nExpect: none, because a test plan", Files: "src/"}}

	if _, err := (application.File{Tracker: tracker}).Run(context.Background(), plan); err == nil ||
		!strings.Contains(err.Error(), "a whole directory") {
		t.Fatalf("expected files src/ refused as a whole directory, got %v", err)
	}
}

func TestPathSetJudgesOnlyAFilesKeyItWrites(t *testing.T) {
	tracker := aLampasStory(t)
	if err := tracker.SetStoryMetadata(context.Background(), "mw-5r3p30.258", map[string]string{"files": "src/"}); err != nil {
		t.Fatalf("giving the story its old files key: %v", err)
	}
	if _, err := (application.PathSet{Tracker: tracker}).
		Run(context.Background(), "mw-5r3p30.258", "", []string{"model=sonnet"}); err != nil {
		t.Errorf("expected model=sonnet taken over a files key already there, got %v", err)
	}
}

func TestPathSetRefusesAGlobOfMoreThanTheMostFiles(t *testing.T) {
	rigs, files := lampasHost()
	for key, count := range map[string]string{
		"tests/e2e/*.ts": "25", "src/Talk.tsx,tests/e2e/*.ts,features/steps/*.ts": "25",
		"src/*.tsx": "21", "tests/**/*.ts": "25", "tests/e2e/spec*.ts": "25",
	} {
		tracker, err := setFiles(t, rigs, files, key)
		if err == nil {
			t.Errorf("expected files=%s to be refused", key)
			continue
		}
		for _, said := range []string{"name the files instead", "matches " + count + " files", "nothing was written"} {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("files=%s: expected the refusal to say %q, got %q", key, said, err)
			}
		}
		if detail, _ := tracker.ShowStory(context.Background(), "mw-5r3p30.258"); detail.Story.Overrides.Files != "" {
			t.Errorf("files=%s: expected nothing written, got %q", key, detail.Story.Overrides.Files)
		}
	}
}

func TestPathSetTakesAGlobOfExactlyTheMostFiles(t *testing.T) {
	files := apptest.NewFakeRigFiles()
	for n := 0; n < application.MostFilesInADirectory; n++ {
		files.Add("/rigs/lampas", fmt.Sprintf("tests/e2e/spec%d.ts", n))
	}
	if _, err := setFiles(t, map[string]string{"lampas": "/rigs/lampas"}, files, "tests/e2e/*.ts"); err != nil {
		t.Errorf("expected a glob of %d files to be taken, got %v", application.MostFilesInADirectory, err)
	}
}

func TestPathSetRefusesABareWildcardGlobWhereTheRigIsNotCheckedOut(t *testing.T) {
	for _, key := range []string{"tests/e2e/*.ts", "features/steps/*.tsx", "src/*/*.ts"} {
		_, err := setFiles(t, nil, nil, key)
		if err == nil {
			t.Errorf("expected files=%s to be refused with no checkout to count it in", key)
			continue
		}
		for _, said := range []string{key, "not checked out here", "name the files instead"} {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("files=%s: expected the refusal to say %q, got %q", key, said, err)
			}
		}
	}
	for _, key := range []string{"src/Talk.tsx", "src/*/Talk.tsx", "src/settings/a.ts,src/settings/b.ts"} {
		if _, err := setFiles(t, nil, nil, key); err != nil {
			t.Errorf("expected files=%s to be taken with no checkout, got %v", key, err)
		}
	}
}

func TestFileRefusesAGlobOfMoreThanTheMostFiles(t *testing.T) {
	rigs, files := lampasHost()
	tracker := apptest.NewFakeTracker()
	tracker.AddFormula("tdd-feature")
	plan := twoStoryPlan()
	plan.Epic.Defaults.Rig = "lampas"
	plan.Stories = []domain.PlanStory{{Key: "talk", Title: "Talk sends twice", Acceptance: "make test passes\nExpect: none, because a test plan",
		Files: "tests/e2e/*.ts"}}

	_, err := application.File{Tracker: tracker, Rigs: rigs, RigFiles: files}.Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "talk") || !strings.Contains(err.Error(), "name the files instead") {
		t.Fatalf("expected the plan refused for its glob, got %v", err)
	}
	if epics := tracker.Epics(); len(epics) != 0 {
		t.Errorf("expected nothing filed, got %v", epics)
	}
}

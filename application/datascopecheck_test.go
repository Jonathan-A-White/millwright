package application_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1589: mw check of a story that changes only a rig's data files runs the
// data's own check, as the one part of its gate, in place of the rig's parts.

// dataFilesChanged is the files a story's branch changes.
type dataFilesChanged []string

func (f dataFilesChanged) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return f, nil
}
func (dataFilesChanged) AddedFiles(context.Context, string, string, string) ([]application.AddedFile, error) {
	return nil, nil
}
func (dataFilesChanged) DiffLines(context.Context, string, string, string, string) ([]application.DiffLine, error) {
	return nil, nil
}

// dataRig is a rig whose gate is two parts, with a data key, that passes every
// command and remembers which it was asked to run.
type dataRig struct {
	mu  sync.Mutex
	ran []string
}

func (d *dataRig) Parts(string) []application.GatePart {
	return []application.GatePart{{Name: "lint", Command: "lint"}, {Name: "unit", Command: "unit"}}
}

func (d *dataRig) DataScope(string) (application.DataScope, bool) {
	return application.DataScope{Paths: []string{"data/lxx/**", "docs/lxx-*.md"}, Command: "data-check"}, true
}

func (d *dataRig) RunLine(_ context.Context, _, _, command string) (application.Checked, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ran = append(d.ran, command)
	return application.Checked{Command: command, Passed: true}, nil
}

func (d *dataRig) commands() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.ran, ",")
}

// aCheckOfFiles is mw check, at part, of a story whose branch changes files.
func aCheckOfFiles(t *testing.T, gate *dataRig, records *mapRecords, files []string, part int) (string, application.CheckReport, error) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	tracker.AddFormula("tdd-feature", application.FormulaStep{ID: "s-1", Title: "Run the full build, tests and vet"})
	poured, err := tracker.PourFormula(context.Background(), "tdd-feature", "mw-x.1", "One", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}); err != nil {
		t.Fatal(err)
	}
	tip := "abc1234abc1234abc1234abc1234abc1234abc12"
	var out bytes.Buffer
	report, err := application.Check{
		Tracker: tracker, Landing: &headedLanding{fakeRetryLanding: &fakeRetryLanding{AheadCount: 1}, head: tip},
		Checks: &scriptedChecks{passes: []bool{true}}, Changes: dataFilesChanged(files),
		Parts: gate, Records: records, Part: part,
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  &out,
	}.Run(context.Background(), "mw-x.1")
	return out.String(), report, err
}

func TestACheckOfAStoryChangingOnlyDataFilesRunsTheDataCheckAloneAndSaysSo(t *testing.T) {
	gate := &dataRig{}
	said, _, err := aCheckOfFiles(t, gate, &mapRecords{}, []string{"data/lxx/choices/gen-01.tsv", "docs/lxx-notes.md"}, 1)
	if err != nil {
		t.Fatalf("expected the part to pass, got %v\n%s", err, said)
	}
	if got := gate.commands(); got != "data-check" {
		t.Errorf("expected the data command alone to run, got %q", got)
	}
	for _, want := range []string{"data scope  2 changed file(s)", "part 1 of 1 (data scope) passed"} {
		if !strings.Contains(said, want) {
			t.Errorf("expected the check to print %q, got:\n%s", want, said)
		}
	}
}

func TestACheckOfAStoryWithOneFileOutsideTheDataPathsRunsTheRigsParts(t *testing.T) {
	gate := &dataRig{}
	said, _, err := aCheckOfFiles(t, gate, &mapRecords{}, []string{"data/lxx/choices/gen-01.tsv", "src/review.ts"}, 2)
	if err != nil {
		t.Fatalf("expected the part to pass, got %v\n%s", err, said)
	}
	if got := gate.commands(); got != "unit" {
		t.Errorf("expected the rig's part 2 to run, got %q", got)
	}
	if strings.Contains(said, "data scope") {
		t.Errorf("expected no word of a data scope, got:\n%s", said)
	}
}

// A story left with steps open says, of a data-scope story, that its one part is
// the data check, and not the rig's parts.
func TestOpenStepsOfADataScopeStoryNameItsOnePartNotTheRigsParts(t *testing.T) {
	gate := &dataRig{}
	_, report, _ := aCheckOfFiles(t, gate, &mapRecords{}, []string{"data/lxx/choices/gen-01.tsv"}, 0)
	var open *application.Refusal
	for i := range report.Refusals {
		if report.Refusals[i].Reason == application.ReasonOpenSteps {
			open = &report.Refusals[i]
		}
	}
	if open == nil {
		t.Fatalf("expected an open-steps refusal, got %+v", report.Refusals)
	}
	for _, want := range []string{"its data scope", "part 1 (data scope) has not been run", "mw check mw-x.1 --part 1"} {
		if !strings.Contains(open.Said, want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, open.Said)
		}
	}
	if strings.Contains(open.Said, "(lint)") || strings.Contains(open.Said, "(unit)") {
		t.Errorf("expected none of the rig's parts named, got:\n%s", open.Said)
	}
}

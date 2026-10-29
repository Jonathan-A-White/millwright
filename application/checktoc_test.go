package application_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// someDocsChanges is a branch that changes the docs it holds, by path.
type someDocsChanges map[string]string

func (s someDocsChanges) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	var paths []string
	for path := range s {
		paths = append(paths, path)
	}
	return paths, nil
}

func (someDocsChanges) AddedFiles(context.Context, string, string, string) ([]application.AddedFile, error) {
	return nil, nil
}

func (someDocsChanges) DiffLines(context.Context, string, string, string, string) ([]application.DiffLine, error) {
	return nil, nil
}

func (s someDocsChanges) ReadBranchFile(_ context.Context, _, _, path string) ([]byte, bool, error) {
	text, ok := s[path]
	return []byte(text), ok, nil
}

// aCheckOfDocs is mw check of a story whose branch changes the docs given, with
// the rig's tests passing.
func aCheckOfDocs(t *testing.T, docs someDocsChanges) (application.CheckReport, error, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	var out strings.Builder
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		Changes: docs,
		Host:    "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.MultiWriter(io.Discard, &out),
	}.Run(context.Background(), "mw-x.1")
	return report, err, out.String()
}

const docWithTheMarkersAndNoHeadings = "# Title\n\n<!-- toc -->\n\n- [Old](#old)\n\n<!-- /toc -->\n\n## New\n"

func TestCheckRefusesAChangedDocWithAStaleTableOfContentsAndPrintsTheCure(t *testing.T) {
	report, err, out := aCheckOfDocs(t, someDocsChanges{"docs/a.md": docWithTheMarkersAndNoHeadings})

	if err == nil || report.Passed() {
		t.Fatalf("expected the check to fail on the stale table, got err %v and %+v", err, report)
	}
	if len(report.Refusals) != 1 || report.Refusals[0].Reason != application.ReasonTOCStale {
		t.Fatalf("expected one %s refusal, got %+v", application.ReasonTOCStale, report.Refusals)
	}
	if !strings.Contains(out, "mw toc fix docs/a.md") {
		t.Errorf("expected the check to print the cure, got:\n%s", out)
	}
}

func TestCheckOfChangedDocsWithCurrentTablesIsUnchanged(t *testing.T) {
	report, err, _ := aCheckOfDocs(t, someDocsChanges{"docs/a.md": "# Title\n\n## One\n\ntext\n", "README.md": "<!-- toc: none -->\n"})
	if err != nil || !report.Passed() {
		t.Fatalf("expected the check to pass, got err %v and %+v", err, report)
	}
}

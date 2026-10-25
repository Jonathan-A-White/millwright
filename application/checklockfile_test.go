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

// aLockfileChanges is a branch's changes that hold only the lines of its lockfile.
type aLockfileChanges struct{ lines []application.DiffLine }

func (aLockfileChanges) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}

func (aLockfileChanges) AddedFiles(context.Context, string, string, string) ([]application.AddedFile, error) {
	return nil, nil
}

func (a aLockfileChanges) DiffLines(context.Context, string, string, string, string) ([]application.DiffLine, error) {
	return a.lines, nil
}

// aCheckOfALockfile is mw check of a story whose branch changes the lockfile
// by the lines given, with the rig's tests passing.
func aCheckOfALockfile(t *testing.T, lines []application.DiffLine) (application.CheckReport, error, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	var out strings.Builder
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		Changes: aLockfileChanges{lines: lines},
		Host:    "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.MultiWriter(io.Discard, &out),
	}.Run(context.Background(), "mw-x.1")
	return report, err, out.String()
}

func TestCheckRefusesALockfileWithAGitSSHLineAndPrintsTheSedThatMendsIt(t *testing.T) {
	line := `      "resolved": "git+ssh://git@github.com/o/a.git#abc",`
	report, err, out := aCheckOfALockfile(t, []application.DiffLine{{Path: "package-lock.json", Text: line, Added: true}})

	if err == nil || report.Passed() {
		t.Fatalf("expected the check to fail on the git+ssh line, got err %v and %+v", err, report)
	}
	if len(report.Refusals) != 1 || report.Refusals[0].Reason != application.ReasonLockfileSSH {
		t.Fatalf("expected one %s refusal, got %+v", application.ReasonLockfileSSH, report.Refusals)
	}
	for _, want := range []string{
		"package-lock.json",
		"git+ssh://git@github.com/o/a.git#abc",
		"sed -i 's#git+ssh://git@github.com/#git+https://github.com/#g' package-lock.json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the check to print %q, got:\n%s", want, out)
		}
	}
}

func TestCheckOfALockfileWithNoGitSSHLineIsUnchanged(t *testing.T) {
	report, err, _ := aCheckOfALockfile(t, []application.DiffLine{
		{Path: "package-lock.json", Text: `      "resolved": "git+https://github.com/o/a.git#abc",`, Added: true},
	})
	if err != nil || !report.Passed() {
		t.Fatalf("expected the check to pass, got err %v and %+v", err, report)
	}
}

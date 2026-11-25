package application_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// The two marker phrases are written in pieces, so that this file's own added
// lines do not say they are unfinished.
const (
	markWord   = "PROVI" + "SIONAL"
	markPhrase = "still to be " + "read"
)

// someAddedLines is a branch's added lines, whatever the base and branch asked.
type someAddedLines struct{ lines []application.AddedLine }

func (s someAddedLines) AddedLines(context.Context, string, string, string) ([]application.AddedLine, error) {
	return s.lines, nil
}

// aCheckWithAddedLines is mw check of a story whose branch adds the lines given,
// with the rig's tests passing and the comments given written on the story.
func aCheckWithAddedLines(t *testing.T, lines []application.AddedLine, comments ...string) (application.CheckReport, error, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	for _, text := range comments {
		if err := tracker.CommentOnStoryAt("mw-x.1", text, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		AddedLines: someAddedLines{lines: lines},
		Host:       "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.MultiWriter(io.Discard, &out),
	}.Run(context.Background(), "mw-x.1")
	return report, err, out.String()
}

func theJohnLine() application.AddedLine {
	return application.AddedLine{Path: "grinds/enduringword.go", Number: 42, Text: "// " + markWord + ": only John 3"}
}

func aClosingCommentWith(lines ...string) string {
	return "Built it.\n" + strings.Join(lines, "\n") + "\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing"
}

func TestCheckRefusesABranchThatAddsAnUnfinishedLineAndNamesFileAndLine(t *testing.T) {
	report, err, out := aCheckWithAddedLines(t, []application.AddedLine{theJohnLine()})

	if err == nil || report.Passed() {
		t.Fatalf("expected the check to fail on the added marker line, got err %v and %+v", err, report)
	}
	if len(report.Refusals) != 1 || report.Refusals[0].Reason != application.ReasonUnfinishedMark {
		t.Fatalf("expected one %s refusal, got %+v", application.ReasonUnfinishedMark, report.Refusals)
	}
	for _, want := range []string{"grinds/enduringword.go:42", "only John 3", "Accepted gap:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected the check to print %q, got:\n%s", want, out)
		}
	}
}

func TestCheckRefusesAStillToBeReadLineInAnyCase(t *testing.T) {
	line := application.AddedLine{Path: "web/app.ts", Number: 7, Text: "  // Chapters 4-21 are " + strings.ToUpper(markPhrase)}
	report, _, out := aCheckWithAddedLines(t, []application.AddedLine{line})
	if len(report.Refusals) != 1 || report.Refusals[0].Reason != application.ReasonUnfinishedMark || !strings.Contains(out, "web/app.ts:7") {
		t.Fatalf("expected an %s refusal naming web/app.ts:7, got %+v\n%s", application.ReasonUnfinishedMark, report.Refusals, out)
	}
}

func TestCheckPassesAnUnfinishedLineTheClosingCommentAcceptsWithAReason(t *testing.T) {
	comment := aClosingCommentWith("Accepted gap: John 3 only; the Mayor files the other chapters as .4.")
	report, err, out := aCheckWithAddedLines(t, []application.AddedLine{theJohnLine()}, comment)
	if err != nil || !report.Passed() {
		t.Fatalf("expected the check to pass with an accepted gap, got err %v, %+v\n%s", err, report, out)
	}
}

func TestAnAcceptedGapWithNoReasonAcceptsNothing(t *testing.T) {
	for name, line := range map[string]string{"bare": "Accepted gap:", "blank": "Accepted gap:   ", "not at the line's start": "We have no Accepted gap: here"} {
		report, _, _ := aCheckWithAddedLines(t, []application.AddedLine{theJohnLine()}, aClosingCommentWith(line))
		if len(report.Refusals) != 1 || report.Refusals[0].Reason != application.ReasonUnfinishedMark {
			t.Errorf("%s: expected the %s refusal to stand, got %+v", name, application.ReasonUnfinishedMark, report.Refusals)
		}
	}
}

func TestAnAcceptedGapAsABulletOrBoldLineIsRead(t *testing.T) {
	for _, line := range []string{"- Accepted gap: only John 3", "**Accepted gap:** only John 3", "accepted gap: only John 3"} {
		report, err, _ := aCheckWithAddedLines(t, []application.AddedLine{theJohnLine()}, aClosingCommentWith(line))
		if err != nil || !report.Passed() {
			t.Errorf("%q: expected the gap to be accepted, got err %v, %+v", line, err, report.Refusals)
		}
	}
}

func TestMarkersInDocsAndMarkdownAreNotRefused(t *testing.T) {
	report, err, out := aCheckWithAddedLines(t, []application.AddedLine{
		{Path: "docs/plans/enduring-word.md", Number: 3, Text: "Chapters 4-21 are " + markPhrase},
		{Path: "docs/notes.txt", Number: 9, Text: markWord + " numbers"},
		{Path: "README.md", Number: 12, Text: markWord},
		{Path: "web/docs/guide.html", Number: 1, Text: markWord},
	})
	// web/docs/guide.html is under a docs/ directory too.
	if err != nil || !report.Passed() {
		t.Fatalf("expected no refusal for docs and markdown, got err %v, %+v\n%s", err, report.Refusals, out)
	}
}

func TestACodeFileNextToAMarkdownOneIsStillRefused(t *testing.T) {
	report, _, _ := aCheckWithAddedLines(t, []application.AddedLine{
		{Path: "README.md", Number: 12, Text: markWord},
		{Path: "src/docs.go", Number: 5, Text: "// " + markWord},
	})
	if len(report.Refusals) != 1 || !strings.Contains(report.Refusals[0].Why, "src/docs.go:5") || strings.Contains(report.Refusals[0].Why, "README.md") {
		t.Fatalf("expected only src/docs.go:5 to be named, got %+v", report.Refusals)
	}
}

func TestALineThatIsNotAddedIsNeverRefused(t *testing.T) {
	// A line the branch leaves as context or removes is not an added line: the
	// port reads only the added ones, so a branch with none is clean.
	report, err, _ := aCheckWithAddedLines(t, nil)
	if err != nil || !report.Passed() {
		t.Fatalf("expected a branch with no added lines to pass, got err %v, %+v", err, report.Refusals)
	}
}

func TestAnAddedLineWithNoMarkerIsNotRefused(t *testing.T) {
	report, err, _ := aCheckWithAddedLines(t, []application.AddedLine{{Path: "a.go", Number: 1, Text: "// the whole of John is read"}})
	if err != nil || !report.Passed() {
		t.Fatalf("expected a clean line to pass, got err %v, %+v", err, report.Refusals)
	}
}

func TestAnUnfinishedMarkHandsItsBranchOnButGivesNoClaimBack(t *testing.T) {
	if !application.ContinuesFrom(application.ReasonUnfinishedMark) {
		t.Errorf("expected %s to continue its branch", application.ReasonUnfinishedMark)
	}
}

// aCheckOfAFinishedFormulaWithAddedLines is mw check of a story whose formula
// was poured and has no step left open, whose branch adds the lines given and
// which carries no comment at all.
func aCheckOfAFinishedFormulaWithAddedLines(t *testing.T, lines []application.AddedLine) (application.CheckReport, error, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	tracker.AddFormula("tdd-feature")
	poured, err := tracker.PourFormula(context.Background(), "tdd-feature", "mw-x.1", "A story", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.SetStoryMetadata(context.Background(), "mw-x.1", map[string]string{application.MoleculeField: poured.RootID}); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	report, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: &scriptedChecks{passes: []bool{true}},
		AddedLines: someAddedLines{lines: lines},
		Host:       "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.MultiWriter(io.Discard, &out),
	}.Run(context.Background(), "mw-x.1")
	return report, err, out.String()
}

// A Builder who has not yet written the closing comment is told of the marker
// and of the missing comment in the one check, not of the marker first and the
// comment on a later run (mw-gq6.1718).
func TestAMarkedBranchWithNoClosingCommentIsToldOfBothInOneCheck(t *testing.T) {
	report, err, out := aCheckOfAFinishedFormulaWithAddedLines(t, []application.AddedLine{theJohnLine()})
	if err == nil || report.Passed() {
		t.Fatalf("expected the check to fail, got err %v and %+v", err, report)
	}
	var reasons []application.Reason
	for _, refusal := range report.Refusals {
		reasons = append(reasons, refusal.Reason)
	}
	if len(reasons) != 2 || reasons[0] != application.ReasonUnfinishedMark || reasons[1] != application.ReasonNoClosingComment {
		t.Fatalf("expected %s and %s, got %v\n%s", application.ReasonUnfinishedMark, application.ReasonNoClosingComment, reasons, out)
	}
}

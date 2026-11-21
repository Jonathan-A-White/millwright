package application_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1578: a criterion whose own text hands it to the Mayor when the
// Builder's host cannot run it is not an unmet criterion when the Builder says
// it handed it over.
const liveDryRunAcceptance = "1. It builds.\n" +
	"2. Live, read-only: mw budget sync --dry-run against the real sheet. If the Builder's host has no gdrive sign-in or YNAB token, say so in the closing comment instead and the Mayor runs it.\n" +
	"3. make test passes."

func TestAnACTheMayorRunsAndTheBuilderHandedOverIsNotUnmet(t *testing.T) {
	comment := "AC2 (live dry run) NOT run: this host has no age key; handed to the Mayor to run.\nFor the rig memory: nothing"
	unmet, handed := application.UnmetCriteriaAfterHandoff(comment, liveDryRunAcceptance)
	if len(unmet) != 0 {
		t.Fatalf("expected no unmet criterion, got %v", unmet)
	}
	if !reflect.DeepEqual(handed.ACs, []int{2}) {
		t.Fatalf("expected AC 2 handed to the Mayor, got %v", handed.ACs)
	}
}

func TestAnACTheMayorDoesNotRunStillStopsTheStoryWhateverTheBuilderSays(t *testing.T) {
	for name, c := range map[string]struct{ comment, acceptance string }{
		"the AC names no Mayor": {"AC3 NOT run here; handed to the Mayor to run.", liveDryRunAcceptance},
		"no hand-over said":     {"AC2 NOT run: this host has no age key.", liveDryRunAcceptance},
		"another AC beside it":  {"AC2 and AC3 NOT run; handed to the Mayor to run.", liveDryRunAcceptance},
		"an AC not in the list": {"AC9 NOT run; handed to the Mayor to run.", liveDryRunAcceptance},
	} {
		unmet, handed := application.UnmetCriteriaAfterHandoff(c.comment, c.acceptance)
		if len(unmet) == 0 || len(handed.ACs) != 0 {
			t.Errorf("%s: expected the criterion unmet and none handed over, got unmet %v handed %v", name, unmet, handed.ACs)
		}
	}
}

func TestAHandedACIsWrittenOnTheBeadAsAMayorToRunLine(t *testing.T) {
	_, handed := application.UnmetCriteriaAfterHandoff("AC2 NOT run: no age key; handed to the Mayor.", liveDryRunAcceptance)
	lines := handed.Comment()
	if !strings.HasPrefix(lines, "MAYOR TO RUN: AC2") || !strings.Contains(lines, "mw budget sync --dry-run") {
		t.Errorf("expected a MAYOR TO RUN: AC2 line carrying the criterion, got %q", lines)
	}
}

func TestACloseOutLandsAStoryWhoseUnmetACTheMayorRuns(t *testing.T) {
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)
	mustDo(t, tracker.SetAcceptance("mw-gq6.9", liveDryRunAcceptance))
	committed := time.Date(2026, 11, 17, 14, 41, 0, 0, time.UTC).Add(-30 * time.Hour)
	mustDo(t, tracker.CommentOnStoryAt("mw-gq6.9",
		"Built it.\nAC2 (live dry run) NOT run: this host has no age key; handed to the Mayor to run.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing",
		committed.Add(2*time.Hour)))
	report, err := theMayorLands(t, tracker, lands, func(n *application.Next) { n.RescopedByMayor = "the acceptance was worded after the claim" })
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", err, report)
	}
	said := strings.Join(tracker.Comments("mw-gq6.9"), "\n")
	if !strings.Contains(said, "MAYOR TO RUN: AC2") {
		t.Errorf("expected a MAYOR TO RUN: AC2 line on the bead, got:\n%s", said)
	}
}

func TestACloseOutStillStopsAStoryWhoseUnmetACTheMayorDoesNotRun(t *testing.T) {
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)
	mustDo(t, tracker.SetAcceptance("mw-gq6.9", liveDryRunAcceptance))
	committed := time.Date(2026, 11, 17, 14, 41, 0, 0, time.UTC).Add(-30 * time.Hour)
	mustDo(t, tracker.CommentOnStoryAt("mw-gq6.9",
		"Built it.\nAC3 NOT run: handed to the Mayor to run.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing",
		committed.Add(2*time.Hour)))
	report, _ := theMayorLands(t, tracker, lands, func(n *application.Next) { n.RescopedByMayor = "the acceptance was worded after the claim" })
	if report.Landed {
		t.Fatalf("expected the story stopped for its unmet criterion, got %+v", report)
	}
	if strings.Contains(strings.Join(tracker.Comments("mw-gq6.9"), "\n"), "MAYOR TO RUN") {
		t.Errorf("expected no MAYOR TO RUN line for a criterion the Mayor does not run")
	}
}

func TestVerifyListShowsAMayorToRunLineUnderTheLanding(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Closed: []application.ClosedStory{verifyClosed("s-run", "", "landed", time.Hour), verifyClosed("s-met", "", "landed", time.Hour)},
		Comments: map[string][]application.Comment{
			"s-run": {{Text: "Internal: nothing for the Governor to look at"}, {Text: "MAYOR TO RUN: AC2: mw budget sync --dry-run, the Mayor runs it"}},
			"s-met": {{Text: "MAYOR TO RUN: AC2: mw budget sync --dry-run, the Mayor runs it"}, {Author: "mayor@vps", Text: "MET-BY-MAYOR: AC 2: ran it"}},
		},
	}
	got, err := application.VerifyCandidates(context.Background(), beads, verifyNow, 7*24*time.Hour)
	if err != nil || len(got) != 1 || got[0].Bead != "s-run" || !strings.Contains(got[0].Check, "MAYOR TO RUN: AC2") {
		t.Fatalf("expected s-run listed with its MAYOR TO RUN line and s-met not, got %+v (%v)", got, err)
	}
}

func TestACloseOutFilesNoBugForAnACTheMayorRuns(t *testing.T) {
	_, tracker, lands, _ := aStoryGivenBackAfterItsLeaseRanOut(t, true)
	mustDo(t, tracker.SetAcceptance("mw-gq6.9", liveDryRunAcceptance))
	committed := time.Date(2026, 11, 17, 14, 41, 0, 0, time.UTC).Add(-30 * time.Hour)
	mustDo(t, tracker.CommentOnStoryAt("mw-gq6.9",
		"Built it.\nAC 2 NOT met here: no age key; handed to the Mayor to run.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory:\nnothing",
		committed.Add(2*time.Hour)))
	report, err := theMayorLands(t, tracker, lands, func(n *application.Next) { n.RescopedByMayor = "the acceptance was worded after the claim" })
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", err, report)
	}
	for _, note := range report.Notes {
		if strings.Contains(note, "landed without its AC") {
			t.Errorf("expected no bug filed for a criterion the Mayor runs, got note %q", note)
		}
	}
}

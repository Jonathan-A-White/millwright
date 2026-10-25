package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// fakeSource is application.SourceText over a map of source directory to the
// text of everything under it; a directory it does not hold is absent.
type fakeSource map[string]string

func (s fakeSource) Contains(dir, text string) (bool, error) {
	body, ok := s[dir]
	if !ok {
		return false, application.ErrNoSource
	}
	return strings.Contains(body, text), nil
}

func (s fakeSource) ContainsInOrder(dir string, pieces []string) (bool, error) {
	body, ok := s[dir]
	if !ok {
		return false, application.ErrNoSource
	}
	for _, piece := range pieces {
		at := strings.Index(body, piece)
		if at < 0 {
			return false, nil
		}
		body = body[at+len(piece):]
	}
	return true, nil
}

const checkItSteps = "Built the vote card.\n\n" +
	"HOW TO CHECK IT, for the Governor\n" +
	"1. Open the app and tap \"Vote now\".\n" +
	"2. The card says \"Thanks for voting\" and a button `Undo` shows.\n" +
	"What's new: New: a vote card.\n" +
	"For the rig memory: nothing\n"

func runLintCheckIt(t *testing.T, comment string, src fakeSource) (application.LintCheckItResult, string, error) {
	t.Helper()
	beads := &apptest.FakeVerifyBeads{
		Details: map[string]application.StoryDetail{
			"lb-1": {Story: domain.Story{ID: "lb-1", Overrides: domain.Path{Rig: "leaderboard"}}, Status: "closed"},
		},
		Comments: map[string][]application.Comment{"lb-1": {{Author: "builder", Text: comment}}},
	}
	out := &strings.Builder{}
	result, err := application.LintCheckIt{
		Beads: beads, Src: src, Rigs: map[string]string{"leaderboard": "/rigs/leaderboard"}, Out: out,
	}.Run(context.Background(), "lb-1")
	return result, out.String(), err
}

func TestLintCheckItPassesWhenEveryQuotedLabelIsInTheSource(t *testing.T) {
	src := fakeSource{"/rigs/leaderboard/src": "<button>Vote now</button> <p>Thanks for voting</p> <b>Undo</b>"}

	result, out, err := runLintCheckIt(t, checkItSteps, src)

	if err != nil {
		t.Fatal(err)
	}
	if result.Failed() || len(result.Quoted) != 3 {
		t.Fatalf("expected 3 labels all found, got %+v\n%s", result, out)
	}
	if !strings.Contains(out, "lint-checkit lb-1: 3 quoted labels, 3 found in src/") {
		t.Fatalf("expected the summary line, got:\n%s", out)
	}
}

func TestLintCheckItFailsNamingTheLabelsNotInTheSource(t *testing.T) {
	src := fakeSource{"/rigs/leaderboard/src": "<button>Vote now</button> <p>Thanks for voting</p>"}

	result, out, err := runLintCheckIt(t, checkItSteps, src)

	if err != nil {
		t.Fatal(err)
	}
	if !result.Failed() || len(result.Missing) != 1 || result.Missing[0] != "Undo" {
		t.Fatalf("expected only Undo missing, got %+v\n%s", result, out)
	}
	if !strings.Contains(out, "not found in src/: Undo") || !strings.Contains(out, "2 found") {
		t.Fatalf("expected the missing label named, got:\n%s", out)
	}
}

func TestLintCheckItReadsOnlyTheHowToCheckItSection(t *testing.T) {
	comment := "Renamed the \"Old Button\" file.\n\n" + checkItSteps + "For the rig memory: gotcha \"ignored quote\"\n"
	src := fakeSource{"/rigs/leaderboard/src": "Vote now Thanks for voting Undo"}

	result, out, err := runLintCheckIt(t, comment, src)

	if err != nil || result.Failed() || len(result.Quoted) != 3 {
		t.Fatalf("expected quotes outside the section left alone, got %+v %v\n%s", result, err, out)
	}
}

func TestLintCheckItAcceptsCurlyQuotes(t *testing.T) {
	comment := "HOW TO CHECK IT, for the Governor\n1. Tap “Vote now”.\n"
	src := fakeSource{"/rigs/leaderboard/src": "Vote now"}

	result, _, err := runLintCheckIt(t, comment, src)

	if err != nil || result.Failed() || len(result.Quoted) != 1 || result.Quoted[0] != "Vote now" {
		t.Fatalf("expected the curly-quoted label found, got %+v %v", result, err)
	}
}

func TestLintCheckItPassesAnInternalCommentWithNoLabels(t *testing.T) {
	result, out, err := runLintCheckIt(t, "Done.\n\nInternal: nothing for the Governor to look at\n", fakeSource{})

	if err != nil || result.Failed() {
		t.Fatalf("expected nothing to check to pass, got %+v %v", result, err)
	}
	if !strings.Contains(out, "no HOW TO CHECK IT") {
		t.Fatalf("expected it to say so, got:\n%s", out)
	}
}

func TestLintCheckItFailsWhenTheRigHasNoSourceDirectory(t *testing.T) {
	_, _, err := runLintCheckIt(t, checkItSteps, fakeSource{})

	if !errors.Is(err, application.ErrNoSource) {
		t.Fatalf("expected ErrNoSource, got %v", err)
	}
}

func TestLintCheckItRefusesABeadWithNoRigOnThisHost(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Details:  map[string]application.StoryDetail{"lb-1": {Story: domain.Story{ID: "lb-1", Overrides: domain.Path{Rig: "leaderboard"}}}},
		Comments: map[string][]application.Comment{"lb-1": {{Text: checkItSteps}}},
	}
	_, err := application.LintCheckIt{Beads: beads, Src: fakeSource{}, Rigs: map[string]string{}, Out: &strings.Builder{}}.Run(context.Background(), "lb-1")

	if err == nil || !strings.Contains(err.Error(), "leaderboard") {
		t.Fatalf("expected an error naming the rig, got %v", err)
	}
}

// mw-lvcm5w.2's shape: the builder's closing comment holds the section, and a
// later VERIFIED note only mentions the words.
func lvcmComments() []application.Comment {
	return []application.Comment{
		{Author: "builder", Created: time.Date(2026, time.October, 24, 9, 30, 0, 0, time.UTC), Text: "Built it.\n\n" +
			"HOW TO CHECK IT, for the Governor:\n" +
			"1. Open the app and tap \"Steps\".\n" +
			"2. Tap \"Back to current step\" and a button `Undo` shows.\n\n" +
			"What's new: none\n\nFor the rig memory:\nnothing\n"},
		{Author: "mw", Created: time.Date(2026, time.October, 24, 12, 0, 0, 0, time.UTC),
			Text: "VERIFIED by the Governor's use: HOW TO CHECK IT posted in his thread, direct:abc"},
	}
}

func TestLintCheckItReadsTheSectionNotALaterNoteThatMentionsTheWords(t *testing.T) {
	beads := &apptest.FakeVerifyBeads{
		Details: map[string]application.StoryDetail{
			"lb-1": {Story: domain.Story{ID: "lb-1", Overrides: domain.Path{Rig: "leaderboard"}}, Status: "closed"},
		},
		Comments: map[string][]application.Comment{"lb-1": lvcmComments()},
	}
	out := &strings.Builder{}
	src := fakeSource{"/rigs/leaderboard/src": "Steps Back to current step Undo"}

	result, err := application.LintCheckIt{
		Beads: beads, Src: src, Rigs: map[string]string{"leaderboard": "/rigs/leaderboard"}, Out: out,
	}.Run(context.Background(), "lb-1")

	if err != nil || result.Failed() || len(result.Quoted) != 3 {
		t.Fatalf("expected the builder's three labels read, got %+v %v\n%s", result, err, out)
	}
	if !strings.Contains(out.String(), "builder") || !strings.Contains(out.String(), "2026-10-24T09:30:00Z") {
		t.Fatalf("expected the comment read named by author and time, got:\n%s", out)
	}
}

func templateComment(labels ...string) string {
	steps := "HOW TO CHECK IT, for the Governor\n"
	for i, label := range labels {
		steps += string(rune('1'+i)) + ". See \"" + label + "\".\n"
	}
	return steps + "For the rig memory: nothing\n"
}

func TestLintCheckItMatchesALabelWithAPlaceholderAroundItsLiteralParts(t *testing.T) {
	cases := []struct{ name, label, source string }{
		{"angle placeholder, template literal", "Show <password>", "<b>{`Show ${name}`}</b>"},
		{"angle placeholder, jsx expression", "Show <password>", "<b>Show ${...}</b>"},
		{"trailing ellipsis", "Step 5 of 9 - ...", "`Step ${i} of ${n} - `"},
		{"unicode ellipsis", "Step 5 of 9 - …", "`Step ${i} of ${n} - `"},
		{"lone N and a full stop", "Just looking - the routine is still on step N.", "`Just looking - the routine is still on step ${n}.`"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result, out, err := runLintCheckIt(t, templateComment(c.label), fakeSource{"/rigs/leaderboard/src": c.source})
			if err != nil {
				t.Fatal(err)
			}
			if result.Failed() || len(result.Skipped) != 0 {
				t.Fatalf("expected %q found, got %+v\n%s", c.label, result, out)
			}
			if !strings.Contains(out, "1 quoted labels, 1 found in src/") {
				t.Fatalf("expected the summary line, got:\n%s", out)
			}
		})
	}
}

func TestLintCheckItFailsATemplateWithAMisspeltLiteralPart(t *testing.T) {
	cases := []struct{ label, source string }{
		{"Show <password>", "Shwo ${name}"},
		{"Step 5 of 9 - ...", "`Step ${i} off ${n} - `"},
		{"Step 5 of 9 - ...", "`Stepp ${i} of ${n} - `"},
		{"Step 5 of 9 - ...", "`${n} of Step ${i} - `"},
	}
	for _, c := range cases {
		result, out, err := runLintCheckIt(t, templateComment(c.label), fakeSource{"/rigs/leaderboard/src": c.source})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Missing) != 1 || result.Missing[0] != c.label {
			t.Fatalf("expected %q missing from %q, got %+v\n%s", c.label, c.source, result, out)
		}
	}
}

func TestLintCheckItSkipsALabelWithNoLiteralPartAndCountsIt(t *testing.T) {
	comment := templateComment("<name>", "N", "Vote now")
	result, out, err := runLintCheckIt(t, comment, fakeSource{"/rigs/leaderboard/src": "<button>Vote now</button>"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed() || len(result.Skipped) != 2 {
		t.Fatalf("expected two skipped and none missing, got %+v\n%s", result, out)
	}
	if !strings.Contains(out, "3 quoted labels, 1 found in src/, 2 skipped (no literal part of 3 characters to look for)") {
		t.Fatalf("expected the skipped count in the summary, got:\n%s", out)
	}
}

func TestLintCheckItNamesSkippedLabelsAlongsideMissingOnes(t *testing.T) {
	comment := templateComment("<name>", "Undo")
	result, out, err := runLintCheckIt(t, comment, fakeSource{"/rigs/leaderboard/src": "nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("got %+v\n%s", result, out)
	}
	if !strings.Contains(out, "2 quoted labels, 0 found in src/, 1 skipped (no literal part of 3 characters to look for), not found in src/: Undo") {
		t.Fatalf("got:\n%s", out)
	}
}

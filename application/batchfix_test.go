package application

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// batch 37 of spell-forge went red on a What's new line mw wrote after its
// fixer had exited: the story's title, which its own test bans, used as the
// note (mw-gq6.942).
func aBriefWithWhatsNewLines(tested bool) BatchFixBrief {
	return BatchFixBrief{
		Rig: "spell-forge", Target: "main", Number: 37, Base: "base0123", Tested: tested,
		Gate: Checked{Command: "make test", Output: "FAIL TestChangelogNeverSaysMaths\n"},
		Stories: []BatchStory{
			{ID: "mw-1", Title: "Kestrel says 'math', never 'maths'", Branch: "origin/mw/mw-1", Merge: "merge111"},
			{ID: "mw-2", Title: "The second", Branch: "origin/mw/mw-2", Merge: "merge222"},
			{ID: "mw-3", Title: "The third", Branch: "origin/mw/mw-3", Merge: "merge333"},
		},
		ChangelogFiles: []string{"public/changelog.json", "CHANGELOG.md"},
		VersionFiles:   []string{"package.json"},
		WhatsNew: []WhatsNewLine{
			{Story: "mw-1", Kind: domain.ChangelogFixed, Text: "Kestrel says 'math', never 'maths'", FromTitle: true},
			{Story: "mw-2", Kind: domain.ChangelogNew, Text: "The second thing works."},
			{Story: "mw-3"},
		},
	}
}

func TestTheFixersBootNamesTheWhatsNewLineMwWritesAfterItExitsForEachStory(t *testing.T) {
	prompt := BatchFixBootPrompt(Seat{Name: "builder", Rig: "spell-forge"}, aBriefWithWhatsNewLines(false))
	for _, want := range []string{
		"## What mw writes into the batch itself",
		"What's new line",
		"public/changelog.json, CHANGELOG.md",
		"after you end, and before it runs the rig's tests",
		"so a test that reads those files meets them",
		"mw-1: fixed: Kestrel says 'math', never 'maths' (the story's title, used as the note because it has no What's new: comment)",
		"mw-2: new: The second thing works.",
		"mw-3: no line (its What's new: none)",
		"`What's new:` comment on the story changes its line",
		"package.json",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the fixer's boot file to hold %q, got:\n%s", want, prompt)
		}
	}
}

func TestTheFixersBootSaysWhenTheWhatsNewLinesAreInTheTreeAlready(t *testing.T) {
	prompt := BatchFixBootPrompt(Seat{Name: "builder", Rig: "spell-forge"}, aBriefWithWhatsNewLines(true))
	if !strings.Contains(prompt, "already in the tree you are in") || strings.Contains(prompt, "after you end, and before") {
		t.Errorf("expected lines written before the tests ran to be said to be in the tree already, got:\n%s", prompt)
	}
}

func TestTheFixersBootSaysNothingOfWhatsNewLinesForARigWithNoChangelog(t *testing.T) {
	brief := aBriefWithWhatsNewLines(false)
	brief.ChangelogFiles, brief.WhatsNew = nil, nil
	prompt := BatchFixBootPrompt(Seat{Name: "builder", Rig: "spell-forge"}, brief)
	if strings.Contains(prompt, "What's new line") {
		t.Errorf("expected no What's new text for a rig that keeps no changelog, got:\n%s", prompt)
	}
}

func aHeldBatchWhoseChangelogMwWrote(output string) BatchReport {
	return BatchReport{
		Rig: "spell-forge", Number: 37, Tested: true,
		Gate:              Checked{Command: "make test", Output: output},
		Fixer:             &BatchFixing{Started: true},
		changelogFiles:    []string{"public/changelog.json", "CHANGELOG.md"},
		changelogWritten:  []domain.ChangelogEntry{{Story: "mw-1", Kind: domain.ChangelogFixed, Text: "Kestrel says 'math', never 'maths'"}},
		writtenAfterFixer: true,
	}
}

func TestTheHeldMailSaysMwWroteTheWhatsNewLineAFailingTestMeets(t *testing.T) {
	report := aHeldBatchWhoseChangelogMwWrote("--- FAIL: TestNoMaths\n    changelog_test.go:12: public/changelog.json says 'maths'\nFAIL")
	got := report.changelogMail()
	for _, want := range []string{"public/changelog.json", "mw wrote", "after the fixer exited", "mw-1", "Kestrel says 'math', never 'maths'"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the changelog part of the mail to hold %q, got %q", want, got)
		}
	}
}

func TestTheHeldMailSaysNothingOfTheChangelogWhenTheFailureIsElsewhere(t *testing.T) {
	report := aHeldBatchWhoseChangelogMwWrote("--- FAIL: TestSpelling\n    spell_test.go:9: expected colour\nFAIL")
	if got := report.changelogMail(); got != "" {
		t.Errorf("expected no changelog text for a failure elsewhere, got %q", got)
	}
	report = BatchReport{Rig: "spell-forge", Number: 37, Tested: true, Gate: Checked{Output: "public/changelog.json"}}
	if got := report.changelogMail(); got != "" {
		t.Errorf("expected no changelog text when mw wrote no line, got %q", got)
	}
}

func TestTheHeldMailDoesNotSayAfterTheFixerWhenTheLineWasWrittenBeforeIt(t *testing.T) {
	report := aHeldBatchWhoseChangelogMwWrote("public/changelog.json says 'maths'")
	report.writtenAfterFixer = false
	got := report.changelogMail()
	if got == "" || strings.Contains(got, "after the fixer exited") {
		t.Errorf("expected the mail to name the line without saying it came after the fixer, got %q", got)
	}
}

// carryLanding is the Landing carryFixer asks: which files a range touches,
// which commits a branch already holds, and what Carry was given.
type carryLanding struct {
	Landing
	files   map[string][]string
	onBr    map[string]bool
	carried [][]string
	err     error
}

func (l *carryLanding) FilesBetween(_ context.Context, _, from, to string) ([]string, error) {
	return l.files[from+".."+to], nil
}

func (l *carryLanding) OnBranch(_ context.Context, _, commit, _ string) (bool, error) {
	return l.onBr[commit], nil
}

func (l *carryLanding) Carry(_ context.Context, _, _ string, commits []string, _, _ string) (string, error) {
	l.carried = append(l.carried, commits)
	return "tip9999", l.err
}

func aFixedBatch(landing *carryLanding, hashes ...string) (BatchLand, *BatchReport, []Commit) {
	report := &BatchReport{
		Base:   "base0123",
		Fixer:  &BatchFixing{},
		Merged: []BatchStory{{ID: "mw-1", Branch: "origin/mw/mw-1"}, {ID: "mw-2", Branch: "origin/mw/mw-2"}},
	}
	landing.files["base0123..origin/mw/mw-1"] = []string{"a.go", "b.go"}
	landing.files["base0123..origin/mw/mw-2"] = []string{"c.go"}
	var commits []Commit
	for _, h := range hashes {
		landing.files[h+"^.."+h] = []string{"a.go"}
		commits = append(commits, Commit{Hash: h, Message: "change " + h})
	}
	return BatchLand{Next: Next{Landing: landing}}, report, commits
}

// batch 468 of lampas: the fixer merged a story's branch, so its commits
// included the branch's own tip, and the cherry-pick of it stopped as empty
// (mw-gq6.1055).
func TestCarryFixerSkipsACommitAlreadyOnTheStorysBranchAndPushesTheRest(t *testing.T) {
	landing := &carryLanding{files: map[string][]string{}, onBr: map[string]bool{"c9dd888": true}}
	b, report, commits := aFixedBatch(landing, "c9dd888", "f1x3d00")

	b.carryFixer(context.Background(), "dir", report, commits)

	if len(landing.carried) != 1 || strings.Join(landing.carried[0], ",") != "f1x3d00" {
		t.Fatalf("expected only the commit the branch lacks to be carried, got %v", landing.carried)
	}
	if len(report.Fixer.CarryTrouble) != 0 || len(report.Notes) != 0 {
		t.Fatalf("expected no trouble, got %v / %v", report.Fixer.CarryTrouble, report.Notes)
	}
	if len(report.Fixer.Carried) != 2 {
		t.Fatalf("expected both commits among the carried, got %+v", report.Fixer.Carried)
	}
}

func TestCarryFixerCarriesNothingWhenTheBranchHoldsEveryCommit(t *testing.T) {
	landing := &carryLanding{files: map[string][]string{}, onBr: map[string]bool{"c9dd888": true}}
	b, report, commits := aFixedBatch(landing, "c9dd888")

	b.carryFixer(context.Background(), "dir", report, commits)

	if len(landing.carried) != 0 {
		t.Fatalf("expected no cherry-pick of a commit the branch holds, got %v", landing.carried)
	}
	if len(report.Fixer.CarryTrouble) != 0 || strings.Contains(strings.Join(report.Notes, "\n"), "could not be pushed") {
		t.Fatalf("expected no 'could not be pushed' line, got %v / %v", report.Fixer.CarryTrouble, report.Notes)
	}
	lines := report.Fixer.carriedLines()
	if !strings.Contains(lines, "already on") || strings.Contains(lines, "No commit of its touched") {
		t.Fatalf("expected the mail to say the commit is already on the branch, got:\n%s", lines)
	}
}

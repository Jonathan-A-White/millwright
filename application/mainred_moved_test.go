package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.967: a branch that adds a line above a failing Playwright test moves
// its line, and `file:line` on main then names no test: the run on main alone
// said "No tests found" and the label "not run". Main's own line of the test
// is found by its title.

const (
	placeSpec    = "tests/e2e/immersive-place.spec.ts"
	placeTitle   = "Immersive on: the verse is placed by the reader"
	otherTitle   = "A test the branch added"
	branchLine   = 299
	mainLine     = 298
	placeFailure = "Running 2 tests using 1 worker\n\n" +
		"  ✘  1 [shots] › tests/e2e/immersive-place.spec.ts:299:7 › " + placeTitle + " (3.1s)\n\n\n" +
		"  1) [shots] › tests/e2e/immersive-place.spec.ts:299:7 › " + placeTitle + " ─────\n\n" +
		"    Error: expect(received).toBe(expected)\n\n  1 failed\n" +
		"    [shots] › tests/e2e/immersive-place.spec.ts:299:7 › " + placeTitle + "\n  1 passed (9.0s)\n"
)

// aSpecOnMain is a worktree of main holding placeSpec with the test titled
// title at line.
func aSpecOnMain(t *testing.T, title string, line int) string {
	t.Helper()
	dir := t.TempDir()
	lines := make([]string, line+3)
	for i := range lines {
		lines[i] = "// filler"
	}
	lines[0] = "import { test } from '@playwright/test'"
	lines[line-1] = "  test('" + title + "', async ({ page }) => {"
	file := filepath.Join(dir, filepath.FromSlash(placeSpec))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type aLandingAt struct {
	aMainLanding
	dir string
}

func (l *aLandingAt) OpenLanding(ctx context.Context, rig, base string) (string, error) {
	if _, err := l.aMainLanding.OpenLanding(ctx, rig, base); err != nil {
		return "", err
	}
	return l.dir, nil
}

type gateSaying struct{ output string }

func (g gateSaying) Run(context.Context, string, string) (application.Checked, error) {
	return application.Checked{Command: "make test", Output: g.output}, nil
}

// aPlaywrightStop is a close-out whose run failed with output, with main at dir.
func aPlaywrightStop(t *testing.T, output, dir string, named application.NamedChecks) closeOutOnMain {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aLandingAt{aMainLanding: aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}, dir: dir}
	mailbox := apptest.NewFakeMailbox()
	got := closeOutOnMain{landing: &landing.aMainLanding, vault: vault}
	got.report, got.err = application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: gateSaying{output}, Slot: aSlot{}, Mailbox: mailbox,
		NamedChecks: named,
		Seat:        "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range inbox {
		got.mail += m.Body + "\n"
	}
	return got
}

// mainRunsOnlyItsOwnLine fails the test only when it is asked for at the line
// main has it at, as Playwright does: any other line names no test.
func mainRunsOnlyItsOwnLine() *namedTestsThat {
	return &namedTestsThat{run: func(_ context.Context, tests []string) (application.NamedRun, error) {
		for _, test := range tests {
			if test == placeSpec+":298" {
				return application.NamedRun{Checked: application.Checked{Command: "playwright test", Output: strings.ReplaceAll(placeFailure, ":299:7", ":298:7")}, Ran: true}, nil
			}
		}
		return application.NamedRun{Checked: application.Checked{Command: "playwright test", Output: "Error: No tests found\n"}, Ran: true}, nil
	}}
}

func TestAPlaywrightTestWhoseLineTheBranchMovedIsRunAloneOnMainByItsTitle(t *testing.T) {
	named := mainRunsOnlyItsOwnLine()
	got := aPlaywrightStop(t, placeFailure, aSpecOnMain(t, placeTitle, mainLine), named)

	if len(named.asked) != 1 || strings.Join(named.asked[0], ",") != placeSpec+":298" {
		t.Fatalf("expected main asked for the test at its own line 298, got %q", named.asked)
	}
	if got.report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red, as main fails the test too, got %q (%v)\nmail:\n%s", got.report.Reason, got.err, got.mail)
	}
	if !strings.Contains(got.mail, placeSpec+":299: fails on main too") {
		t.Errorf("expected the mail to say the test fails on main too, got:\n%s", got.mail)
	}
	if strings.Contains(got.mail, "not run") {
		t.Errorf("expected no 'not run', got:\n%s", got.mail)
	}
	if got.report.MainRed == nil || strings.Join(got.report.MainRed.Tests, ",") != placeSpec+":298" {
		t.Errorf("expected main's red to name the test by main's line, got %+v", got.report.MainRed)
	}
}

func TestAPlaywrightTestThatPassesOnMainAtItsOwnLineIsBranchOnly(t *testing.T) {
	named := &namedTestsThat{run: func(context.Context, []string) (application.NamedRun, error) {
		return application.NamedRun{Checked: application.Checked{Command: "playwright test", Passed: true}, Ran: true}, nil
	}}
	got := aPlaywrightStop(t, placeFailure, aSpecOnMain(t, placeTitle, mainLine), named)
	if len(named.asked) != 1 || strings.Join(named.asked[0], ",") != placeSpec+":298" {
		t.Fatalf("expected main asked for line 298, got %q", named.asked)
	}
	if !strings.Contains(got.mail, placeSpec+":299: branch only") {
		t.Errorf("expected branch only, got:\n%s", got.mail)
	}
}

func TestAPlaywrightTestTheBranchAddedSaysSoInWordsAndIsNotAsked(t *testing.T) {
	named := mainRunsOnlyItsOwnLine()
	got := aPlaywrightStop(t, placeFailure, aSpecOnMain(t, otherTitle, 120), named)
	if len(named.asked) != 0 {
		t.Errorf("expected no run when no failing test is on main, got %q", named.asked)
	}
	if got.report.Reason != application.ReasonTestsFail {
		t.Fatalf("expected tests-fail as before, got %q (%v)", got.report.Reason, got.err)
	}
	if !strings.Contains(got.mail, placeSpec+":299: not on main: the branch added it") {
		t.Errorf("expected the label in words, got:\n%s", got.mail)
	}
	if strings.Contains(got.mail, "not run") {
		t.Errorf("expected no 'not run', got:\n%s", got.mail)
	}
}

func TestAPlaywrightFileMainDoesNotHaveIsAnAddedTest(t *testing.T) {
	named := mainRunsOnlyItsOwnLine()
	got := aPlaywrightStop(t, placeFailure, t.TempDir(), named)
	if !strings.Contains(got.mail, placeSpec+":299: not on main: the branch added it") {
		t.Errorf("expected the label in words, got:\n%s", got.mail)
	}
}

func TestPlaywrightTitlesAreTheLastWordsOfTheLineWithoutDurationOrRetry(t *testing.T) {
	output := "Running 2 tests using 1 worker\n" +
		"  ✘  1 [shots] › tests/e2e/a.spec.ts:12:5 › Group › one (retry #1) (1.0s)\n" +
		"  ✘  2 [shots] › tests/e2e/b.spec.ts:7:3 › two: (Verse view) (2.0s)\n" +
		"  1) [shots] › tests/e2e/a.spec.ts:12:5 › Group › one ───\n  2) [shots] › tests/e2e/b.spec.ts:7:3 › two: (Verse view) ───\n" +
		"  2 failed\n    [shots] › tests/e2e/a.spec.ts:12:5 › Group › one\n    [shots] › tests/e2e/b.spec.ts:7:3 › two: (Verse view)\n"
	got := application.PlaywrightTitles(output)
	if got["tests/e2e/a.spec.ts:12"] != "one" || got["tests/e2e/b.spec.ts:7"] != "two: (Verse view)" || len(got) != 2 {
		t.Errorf("PlaywrightTitles = %q", got)
	}
}

func TestLineOfTestTitledIsThatOfTheTestCallNearestTheOldLine(t *testing.T) {
	source := "test.describe('one', () => {\n" + // 1: a group, not a test
		"  test('same', async () => {})\n" + // 2
		"  test(\n    'multi',\n    async () => {})\n" + // 3-5
		"})\n" +
		"filler\n\n\n\n\n\n\n" +
		"test('same', async () => {})\n" // 14
	cases := []struct {
		title string
		near  int
		want  int
		found bool
	}{
		{"multi", 40, 3, true},
		{"same", 3, 2, true},
		{"same", 20, 14, true},
		{"one", 1, 0, false},
		{"gone", 1, 0, false},
	}
	for _, c := range cases {
		if got, found := application.LineOfTestTitled(source, c.title, c.near); got != c.want || found != c.found {
			t.Errorf("LineOfTestTitled(%q, near %d) = %d, %v; want %d, %v", c.title, c.near, got, found, c.want, c.found)
		}
	}
}

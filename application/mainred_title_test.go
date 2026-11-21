package application_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// longSteps are the step lines of a vitest scenario like lampas's of
// 2026-11-19, long enough that every failing test's name together is over the
// 500 characters bd takes for a title, even each named once.
var longSteps = []string{
	"Then the reader sees the chapter's first page with its heading, its drop cap and its running footer in place",
	"Then the reader sees the chapter's last page with its closing ornament and the link to the next chapter shown",
	"Then the table of contents lists every chapter of the book with its number, its title and its first page",
	"Then the bookmark the reader left on page twelve is still there after the book is closed and opened again",
	"Then the footnotes of the chapter are numbered from one again and each links back to the line that cites it",
	"Then the reader's place is kept in the book when the window is narrowed to a phone's width and widened again",
}

// aLongRedOutput is a vitest run failing each of longSteps twice over: its bare
// `×` step line and its full `file > Feature > Scenario > step` path, as lampas
// main's red printed them (mw-gq6.1521).
func aLongRedOutput() string {
	var b strings.Builder
	for _, step := range longSteps {
		fmt.Fprintf(&b, "× %s 31ms\n", step)
	}
	for _, step := range longSteps {
		fmt.Fprintf(&b, "FAIL features/reading.feature > Reading a long book > Scenario: the reader turns every page > %s\n", step)
	}
	return b.String()
}

type longFailingTests struct{}

func (longFailingTests) Run(context.Context, string, string) (application.Checked, error) {
	return application.Checked{Command: "make test", Output: aLongRedOutput()}, nil
}

// mw-gq6.1521: lampas main's red named 665 characters of failing tests in the
// bug's title, bd refused it, and the red went unfiled. The bug is filed with
// a title within bd's limit, each test named once, and the whole list kept in
// its description and acceptance.
func TestAMainRedWithLongTestNamesIsFiledWithATitleBdTakes(t *testing.T) {
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", theSourceStory())
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	landing := &aMainLanding{fakeRetryLanding: fakeRetryLanding{AheadCount: 1}}
	named := &namedTestsThat{run: failingOnMain(aLongRedOutput())}
	before := tracker.Stories()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: landing, Landing: landing,
		Checks: longFailingTests{}, Slot: aSlot{}, Mailbox: apptest.NewFakeMailbox(),
		NamedChecks: named, MainAloneBound: 0,
		LoadWait: func(context.Context, time.Duration) error { return nil },
		Seat:     "builder", Host: "laptop",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if report.Reason != application.ReasonMainRed {
		t.Fatalf("expected the story held on main's red, got %q (%v); notes %v", report.Reason, err, report.Notes)
	}
	if report.MainRed == nil || len(report.MainRed.Tests) != len(longSteps) {
		t.Fatalf("expected main's red to name all %d tests (the × lines name none, mw-gq6.1569), got %+v; notes %v", len(longSteps), report.MainRed, report.Notes)
	}
	if all := strings.Join(report.MainRed.Tests, ", "); len(all) <= domain.MaxTitleLength {
		t.Fatalf("the fixture's tests are only %d characters together, not over %d", len(all), domain.MaxTitleLength)
	}

	filed := theTestersFiled(t, tracker, before)
	if len(filed) != 1 {
		t.Fatalf("expected 1 main-red bug filed, got %d: %+v", len(filed), filed)
	}
	bug := filed[0]
	title := bug.Story.Title
	if n := len(title); n > domain.MaxTitleLength {
		t.Errorf("expected a title of at most %d characters, got %d: %q", domain.MaxTitleLength, n, title)
	}
	if !strings.HasPrefix(title, "[bug] main is red: features/reading.feature > Reading a long book > Scenario: the reader turns every page > "+longSteps[0]) {
		t.Errorf("expected the title to start with the first failing test, got %q", title)
	}
	if !strings.HasSuffix(title, " more)") {
		t.Errorf("expected the title to end by counting the tests it leaves out, got %q", title)
	}
	for _, step := range longSteps {
		if n := strings.Count(title, step); n > 1 {
			t.Errorf("expected the title to name %q once, got %d times: %q", step, n, title)
		}
	}
	for _, name := range report.MainRed.Tests {
		if !strings.Contains(bug.Description, name) {
			t.Errorf("expected the description to keep %q, got %q", name, bug.Description)
		}
		if !strings.Contains(bug.Acceptance, name) {
			t.Errorf("expected the acceptance to keep %q, got %q", name, bug.Acceptance)
		}
	}
}

// The title names each distinct test once: a bare step line and the path that
// ends in it are one test, and what does not fit is counted.
func TestMainRedTitleNamesEachTestOnce(t *testing.T) {
	red := application.MainRed{Tests: []string{
		"× Then it works", "× Then it still works",
		"features/a.feature > A > Scenario: s > Then it works",
		"features/a.feature > A > Scenario: s > Then it still works",
	}}
	got := red.Title("[bug] main is red: ")
	want := "[bug] main is red: × Then it works, × Then it still works"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	var many []string
	for i := 0; i < 40; i++ {
		many = append(many, fmt.Sprintf("TestTheReaderSeesPage%02dOfTheLongBookWithEveryOrnament", i))
	}
	got = application.MainRed{Tests: many}.Title("[bug] main is red: ")
	if len(got) > domain.MaxTitleLength {
		t.Errorf("expected at most %d characters, got %d", domain.MaxTitleLength, len(got))
	}
	if !strings.HasPrefix(got, "[bug] main is red: "+many[0]+", "+many[1]) || !strings.HasSuffix(got, " more)") {
		t.Errorf("expected the first names and then the count of the rest, got %q", got)
	}

	one := application.MainRed{Tests: []string{strings.Repeat("x", 700)}}.Title("[bug] main is red: ")
	if len(one) > domain.MaxTitleLength || !strings.HasSuffix(one, domain.Ellipsis) {
		t.Errorf("expected one over-long name cut short with an ellipsis, got %d characters: %q", len(one), one)
	}
}

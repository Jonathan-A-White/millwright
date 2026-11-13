package application_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// stubFindingChecks answers what main shows, and keeps what it was asked.
type stubFindingChecks struct {
	answer  application.FindingAnswer
	err     error
	asked   int
	rig     string
	command string
	files   []application.FindingLine
}

func (s *stubFindingChecks) RecheckFinding(_ context.Context, rig, command string, files []application.FindingLine) (application.FindingAnswer, error) {
	s.asked++
	s.rig, s.command, s.files = rig, command, files
	return s.answer, s.err
}

// landedWithFindings closes mw-x.1 out with comment as its closing comment, a
// FindingChecks of checks, and gives the stories it filed and the Landed mail.
func landedWithFindings(t *testing.T, comment string, checks application.FindingRechecker) (filed []application.StoryDetail, body string, report application.NextReport) {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	mustDo(t, tracker.CommentOnStory(ctx, "mw-x.1", comment))
	before := tracker.Stories()
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aLandingLanding{fakeRetryLanding{AheadCount: 1}}
	mailbox := apptest.NewFakeMailbox()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true}}, Slot: aSlot{}, Mailbox: mailbox,
		RetryWait: func(context.Context, time.Duration) error { return nil },
		Seat:      "builder", Host: "laptop",
		Rigs:          map[string]string{"millwright": "/rigs/millwright"},
		FindingChecks: checks,
		Out:           io.Discard, Err: io.Discard,
	}.Run(ctx, "mw-x.1")
	if err != nil || report.Refused || !report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", err, report)
	}
	mail, _ := mailbox.Inbox(ctx, application.MayorMailbox)
	for _, m := range mail {
		if strings.HasPrefix(m.Subject, application.MailLanded) {
			body = m.Body
		}
	}
	return theTestersFiled(t, tracker, before), body, report
}

const lintFinding = "make lint refuses application/status.go for being 2516 lines, over the 2500 limit (application/status.go:1). " +
	"AC: make lint passes (scripts/check-file-length_test.sh reports ok). Sweep: wc -l application/*.go. Size: S. By design? no"

// A finding whose own command passes on main as merged is fixed already: no bug
// is filed, and the report says so (mw-gq6.1245).
func TestAFindingWhoseCommandPassesOnMainIsNotFiledAndTheReportSaysSo(t *testing.T) {
	main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: true, Tail: "ok"}}

	filed, body, report := landedWithFindings(t, "Done.\nFound, not fixed:\n"+lintFinding+"\nFor the rig memory: nothing", main)

	if len(filed) != 0 {
		t.Fatalf("expected no bug filed for a finding main fixes already, got %+v", filed)
	}
	if main.rig != "millwright" || main.command != "make lint" {
		t.Errorf("expected main asked to run %q for the rig millwright, got %q for %q", "make lint", main.command, main.rig)
	}
	want := "already fixed on main: make lint refuses application/status.go for being 2516 lines, over the 2500 limit (application/status.go:1)"
	if !strings.Contains(body, want) {
		t.Errorf("expected the Landed mail to say %q, got:\n%s", want, body)
	}
	if strings.Contains(body, "Filed: ") {
		t.Errorf("expected no Filed line, got:\n%s", body)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "already fixed on main") {
		t.Errorf("expected a note that it is fixed on main, got %q", report.Notes)
	}
}

// A finding whose command still fails on main is filed as it always was.
func TestAFindingWhoseCommandStillFailsOnMainIsFiled(t *testing.T) {
	main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: false, Tail: "FAIL"}}

	filed, body, _ := landedWithFindings(t, "Done.\nFound, not fixed:\n"+lintFinding+"\nFor the rig memory: nothing", main)

	if main.asked != 1 {
		t.Errorf("expected main asked once, got %d", main.asked)
	}
	if len(filed) != 1 || !strings.Contains(body, "Filed: "+filed[0].Story.ID+", held.") || strings.Contains(body, "already fixed on main") {
		t.Fatalf("expected one held bug filed and no already-fixed line, got %+v and:\n%s", filed, body)
	}
}

// With no command to run, the named file and line are read on main: a line the
// file no longer has is a fault no longer there, and a line it still has is filed.
func TestAFindingWithNoCommandIsReadAtItsFileAndLineOnMain(t *testing.T) {
	finding := "The parser drops a trailing line (application/parse.go:900)"
	comment := "Done.\nFound, not fixed:\n" + finding + "\nFor the rig memory: nothing"

	gone := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Lines: map[string]int{"application/parse.go": 340}}}
	filed, body, _ := landedWithFindings(t, comment, gone)
	if len(filed) != 0 || !strings.Contains(body, "already fixed on main: "+finding) {
		t.Errorf("expected nothing filed and the line named as fixed on main, got %+v and:\n%s", filed, body)
	}
	if gone.command != "" || len(gone.files) != 1 || gone.files[0] != (application.FindingLine{Path: "application/parse.go", Line: 900}) {
		t.Errorf("expected no command and the file and line asked, got %q and %+v", gone.command, gone.files)
	}

	still := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Lines: map[string]int{"application/parse.go": 1200}}}
	if filed, _, _ := landedWithFindings(t, comment, still); len(filed) != 1 {
		t.Errorf("expected a finding whose line main still has to be filed, got %+v", filed)
	}
	missing := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Lines: map[string]int{}}}
	if filed, _, _ := landedWithFindings(t, comment, missing); len(filed) != 1 {
		t.Errorf("expected a finding whose file is not on main to be filed (it may have moved), got %+v", filed)
	}
}

// A command that is not cheap and deterministic is not run, and a test that main
// does not have yet is no pass.
func TestAFindingWhoseCommandIsNotCheapOrDoesNotRunATestIsFiled(t *testing.T) {
	for name, c := range map[string]struct {
		line   string
		answer application.FindingAnswer
	}{
		"a whole suite":          {"A is wrong (a.go:5). AC: make test passes. Sweep: none. Size: S. By design? no", application.FindingAnswer{Ran: true, Passed: true, Lines: map[string]int{"a.go": 100}}},
		"a shell pipeline":       {"A is wrong (a.go:5). AC: go test ./a -run TestA | grep ok passes. Sweep: none. Size: S. By design? no", application.FindingAnswer{Ran: true, Passed: true, Lines: map[string]int{"a.go": 100}}},
		"a test not yet written": {"A is wrong (a.go:5). AC: go test ./a -run TestAKeeps passes. Sweep: none. Size: S. By design? no", application.FindingAnswer{Ran: true, Passed: true, Tail: "ok  \tx/a\t0.003s [no tests to run]", Lines: map[string]int{"a.go": 9}}},
	} {
		t.Run(name, func(t *testing.T) {
			main := &stubFindingChecks{answer: c.answer}
			filed, body, _ := landedWithFindings(t, "Done.\nFound, not fixed:\n"+c.line+"\nFor the rig memory: nothing", main)
			if len(filed) != 1 || strings.Contains(body, "already fixed on main") {
				t.Errorf("expected the finding filed, got %+v and:\n%s", filed, body)
			}
			if main.command == "make test" || strings.Contains(main.command, "|") {
				t.Errorf("expected no run of %q on main", main.command)
			}
		})
	}
}

// A finding mw cannot check (no command, no file and line), or cannot read
// main for, is filed as before; so is every finding when there is no check.
func TestAFindingMwCannotCheckIsFiledAsBefore(t *testing.T) {
	plain := "Done.\nFound, not fixed:\nThe cache is never emptied\nFor the rig memory: nothing"
	main := &stubFindingChecks{answer: application.FindingAnswer{Ran: true, Passed: true}}
	if filed, _, _ := landedWithFindings(t, plain, main); len(filed) != 1 {
		t.Errorf("expected a finding with no command and no file filed, got %+v", filed)
	}
	if main.asked != 0 {
		t.Errorf("expected main not asked about a finding with nothing to check, asked %d times", main.asked)
	}

	broken := &stubFindingChecks{err: errors.New("cutting a worktree of millwright: no tree")}
	filed, _, report := landedWithFindings(t, "Done.\nFound, not fixed:\n"+lintFinding+"\nFor the rig memory: nothing", broken)
	if len(filed) != 1 {
		t.Errorf("expected a finding main could not be read for to be filed, got %+v", filed)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "no tree") {
		t.Errorf("expected a note of why main was not read, got %q", report.Notes)
	}

	if filed, _, _ := landedWithFindings(t, "Done.\nFound, not fixed:\n"+lintFinding+"\nFor the rig memory: nothing", nil); len(filed) != 1 {
		t.Errorf("expected a finding filed when nothing checks main, got %+v", filed)
	}
}

// The suite reads a finding in one throwaway worktree of main: the lines of each
// file it names (a file that is not there has none) and its command, and takes
// the worktree away.
func TestASuiteReadsAFindingOnMainInOneWorktreeAndTakesItAway(t *testing.T) {
	runs := &apptest.FakeSuiteRunner{Respond: func(_, command string) (application.Checked, error) {
		switch {
		case strings.Contains(command, "wc -l < application/status.go"):
			return application.Checked{Passed: true, Output: " 2431\n"}, nil
		case strings.Contains(command, "wc -l"):
			return application.Checked{Passed: false}, nil
		}
		return application.Checked{Passed: true, Output: "ok\n"}, nil
	}}
	suite, trees := recheckSuite(runs)
	suite.Rigs["millwright"] = "/rigs/lampas"

	got, err := suite.RecheckFinding(context.Background(), "millwright", "make lint", []application.FindingLine{
		{Path: "application/status.go", Line: 1}, {Path: "application/status.go", Line: 40}, {Path: "gone.go", Line: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ran || !got.Passed || got.Sha != "9f3c2ab41d0e" || got.Lines["application/status.go"] != 2431 {
		t.Errorf("got %+v, want make lint passed at the main sha and status.go of 2431 lines", got)
	}
	if _, there := got.Lines["gone.go"]; there {
		t.Errorf("expected no line count for a file main does not have, got %+v", got.Lines)
	}
	if ran := runs.Ran(); len(ran) != 3 || ran[2].Command != "make lint" {
		t.Errorf("expected one count a file and then make lint, ran %+v", ran)
	}
	if len(trees.Added()) != 1 || trees.Live() != 0 {
		t.Errorf("one worktree should be cut and taken away, added %v live %d", trees.Added(), trees.Live())
	}

	if _, err := suite.RecheckFinding(context.Background(), "nowhere", "make lint", nil); err == nil {
		t.Errorf("expected an error for a rig this host does not have")
	}
}

// A Tester's bug finding whose acceptance command passes on main as merged is
// fixed already: nothing is filed, and it says so. One whose command fails, one
// whose acceptance names no cheap command, and a taste finding are filed
// (mw-gq6.1245).
func TestATesterFindingWhoseCommandPassesOnMainIsNotFiled(t *testing.T) {
	finding := testerFinding()
	finding.Acceptance = "go test ./application -run TestReaderKeepsItsPage passes\nSweep: every viewport effect in src/ is grepped for state it drops"

	tracker, rules := testerFileEpic(t)
	var out strings.Builder
	main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: true, Tail: "ok  \tx\t0.1s"}}
	filed, err := application.TesterFiler{Tracker: tracker, Rules: rules, Main: main, Out: &out}.File(context.Background(), "mw-e.2", finding)
	if err != nil {
		t.Fatal(err)
	}
	if filed.ID != "" || !strings.Contains(filed.FixedOnMain, "TestReaderKeepsItsPage") || main.rig != "lampas" ||
		main.command != "go test ./application -run TestReaderKeepsItsPage" {
		t.Errorf("expected nothing filed and the command run on main for lampas, got %+v and %q for %q", filed, main.command, main.rig)
	}
	if !strings.Contains(out.String(), "already fixed on main: "+finding.Title) {
		t.Errorf("expected it to say the finding is fixed on main, got %q", out.String())
	}

	for name, c := range map[string]struct {
		change func(*application.TesterFinding)
		answer application.FindingAnswer
	}{
		"the command fails": {func(*application.TesterFinding) {}, application.FindingAnswer{Ran: true, Passed: false}},
		"no cheap command": {func(f *application.TesterFinding) {
			f.Acceptance = "npx vitest run src/Reader.test.tsx passes\nSweep: every viewport effect in src/ is grepped for state it drops"
		}, application.FindingAnswer{Ran: true, Passed: true}},
		"a taste finding":       {func(f *application.TesterFinding) { f.Kind = "taste" }, application.FindingAnswer{Ran: true, Passed: true}},
		"the command is no run": {func(*application.TesterFinding) {}, application.FindingAnswer{Ran: true, Passed: true, Tail: "ok x [no tests to run]"}},
	} {
		t.Run(name, func(t *testing.T) {
			tracker, rules := testerFileEpic(t)
			f := finding
			c.change(&f)
			filed, err := application.TesterFiler{Tracker: tracker, Rules: rules, Main: &stubFindingChecks{answer: c.answer}}.File(context.Background(), "mw-e.2", f)
			if err != nil || filed.ID == "" || filed.FixedOnMain != "" {
				t.Errorf("expected the finding filed, got %+v, %v", filed, err)
			}
		})
	}
	tracker, rules = testerFileEpic(t)
	broken := &stubFindingChecks{err: errors.New("no tree")}
	if filed, err := (application.TesterFiler{Tracker: tracker, Rules: rules, Main: broken}).File(context.Background(), "mw-e.2", finding); err != nil || filed.ID == "" {
		t.Errorf("expected a finding main could not be read for to be filed, got %+v, %v", filed, err)
	}
}

// A finding whose acceptance is a grep is read on main by the grep's exit: a
// match is a pass for 'prints a line', and no match (exit 1, not a missing file)
// is the pass for 'prints nothing'. Anything the shell could read more into is
// not run (mw-gq6.1269).
func TestFoundCheckCommandReadsAGrepWithItsExitMeaning(t *testing.T) {
	for name, c := range map[string]struct {
		ac   string
		want string
	}{
		"a match is a pass":             {`grep -n "ended in error" docs/vultr-boost.md prints a line in the section on the box gate`, `grep -n 'ended in error' docs/vultr-boost.md`},
		"a single-quoted backtick span": {"grep -n '`thaw`' docs/protocol.md finds a row naming what it does", "grep -n '`thaw`' docs/protocol.md"},
		"a backticked command":          {"`grep -c TODO a.go b.go` exits 0", "grep -c 'TODO' a.go b.go"},
		"no match is the pass":          {`grep -n 'time.Now()' application/x.go prints nothing`, `grep -n 'time.Now()' application/x.go; test $? -eq 1`},
		"no line is the pass":           {`grep -rn FooBar src/ finds no lines`, `grep -rn 'FooBar' src/; test $? -eq 1`},
		"a numbered line":               {`1. grep -n foo docs/a.md prints a line`, `grep -n 'foo' docs/a.md`},
		"a pattern and its files":       {`grep -n "a|b" a.go prints a line`, `grep -n 'a|b' a.go`},
		"a shell variable":              {`grep -n "$HOME" a.go prints a line`, ``},
		"a command substitution":        {"grep -n \"`id`\" a.go prints a line", ``},
		"a pipe":                        {`grep -n foo a.go | wc -l prints a line`, ``},
		"a file above the rig":          {`grep -n foo ../a.go prints a line`, ``},
		"a file by an absolute path":    {`grep -n foo /etc/passwd prints a line`, ``},
		"a file in the home":            {`grep -n foo ~/a.go prints a line`, ``},
		"a pattern that is an option":   {`grep -n -- -x a.go prints a line`, ``},
		"no file":                       {`grep -n foo prints a line`, ``},
		"a result that is read":         {`grep -n "78" docs/a.md prints only lines that say 78 is the count`, ``},
		"a number read from the output": {`grep -c foo a.go prints a number lower than 144`, ``},
		"a claim read by a person":      {`grep -n "bsv-kit" docs/a.md names no claim that it depends on it`, ``},
		"a redirect":                    {`grep -n foo a.go > /tmp/x prints a line`, ``},
	} {
		t.Run(name, func(t *testing.T) {
			main := &stubFindingChecks{answer: application.FindingAnswer{Sha: "21bc605c", Ran: true, Passed: true, Tail: ""}}
			line := "A is wrong (a.go:5). AC: " + c.ac + ". Sweep: none. Size: S. By design? no"
			filed, _, _ := landedWithFindings(t, "Done.\nFound, not fixed:\n"+line+"\nFor the rig memory: nothing", main)
			if c.want == "" {
				if main.command != "" {
					t.Errorf("expected no run of %q on main", main.command)
				}
				return
			}
			if main.command != c.want {
				t.Errorf("expected main asked to run %q, got %q", c.want, main.command)
			}
			if len(filed) != 0 {
				t.Errorf("expected a grep that passes on main to file no bug, got %+v", filed)
			}
		})
	}
}

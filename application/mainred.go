package application

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Jonathan-A-White/millwright/domain"
)

// A story whose merged tests fail only because the target branch itself is red
// is not the story's to fix (mw-gq6.645): before a close-out blames the story
// for a gate failure (sends it back, refuses it, counts an attempt) it runs just
// the tests that failed, by name, on the target branch alone. When main fails
// them too, the story is held on main's fix instead, and its close-out is run
// again, alone, once that fix is closed.

// NamedRun is one try at a rig's named-tests line: the tests that failed, run by
// name. Ran is false when nothing was run, because the rig names no line.
type NamedRun struct {
	Checked
	Ran bool
}

// NamedChecks is the port a rig's named-tests line is run through.
type NamedChecks interface {
	// RunNamed runs just the tests named, by name, in dir, as the rig's tests
	// are run. A rig with no named-tests line runs nothing. Tests that fail are
	// a NamedRun that did not pass, not an error.
	RunNamed(ctx context.Context, rig, dir string, tests []string) (NamedRun, error)
}

const (
	// MainRedState is the state dimension that says a story is held on main's
	// own red: its value is the bead that carries the fix, and MainRedFixed
	// once that bead is closed and the close-out is to be run again.
	MainRedState = "mainred"
	MainRedFixed = "fixed"

	// MailMainRed is the verdict a close-out's mail goes under for a story held
	// on main's red, in place of Sent back or Refused.
	MailMainRed = "Main is red"

	// ReasonMainRed is the code a story held on main's red is filed under.
	ReasonMainRed Reason = "main-red"
)

// playwrightFailure is a Playwright line that names a failed test: the list
// reporter's `✘  1 [shots] › tests/e2e/x.spec.ts:106:7 › title (31.0s)` and the
// numbered header of a failure's detail, which the dot and line reporters print,
// `1) [shots] › tests/e2e/x.spec.ts:106:7 › title ───`. The project in brackets
// is optional; the file and the line are kept, the column is not.
var playwrightFailure = regexp.MustCompile(`^\s*(?:✘\s+\d+|\d+\))\s+(?:\[[^\]]*\]\s+›\s+)?(\S+\.spec\.[cm]?[jt]sx?):(\d+):\d+\s+›(.*)$`)

// vitestFailure is vitest's `FAIL  src/a.test.ts > title`: the file it names.
var vitestFailure = regexp.MustCompile(`^FAIL\s+(\S+\.(?:test|spec)\.[cm]?[jt]sx?)\s+>`)

// TestNamesOf is the failing tests of a run's output as a rig's named-tests line
// takes them, each once, in order: go test's `--- FAIL: TestX/sub` as TestX,
// Playwright's `[shots] › tests/e2e/x.spec.ts:106:7 › title` as
// `tests/e2e/x.spec.ts:106` (a retry is the same test), vitest's `FAIL file >
// test` as the file, which `vitest run <file>` runs alone; any other line as it
// is. A name the line can run alone on the target is what lets main's own red
// be found (mw-gq6.934). One function names the tests of the story's run and of
// the run on the target, so the two are compared by one name.
func TestNamesOf(output string) []string {
	failing, _ := FailingTests(output, 1<<30)
	for _, line := range strings.Split(strings.ReplaceAll(domain.StripANSI(WithoutExpectedFailures(output)), "\r\n", "\n"), "\n") {
		// The detail header of a failure, which the dot and line reporters print
		// in place of a ✘ line.
		if playwrightFailure.MatchString(line) {
			failing = append(failing, strings.TrimSpace(line))
		}
	}
	var names []string
	seen := map[string]bool{}
	for _, line := range failing {
		name := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(name, "--- FAIL: "):
			name = strings.Fields(strings.TrimPrefix(name, "--- FAIL: "))[0]
			name, _, _ = strings.Cut(name, "/")
		case vitestFailure.MatchString(name):
			name = vitestFailure.FindStringSubmatch(name)[1]
		case playwrightFailure.MatchString(name):
			m := playwrightFailure.FindStringSubmatch(name)
			name = m[1] + ":" + m[2]
		case strings.HasPrefix(name, "FAIL "):
			name = strings.TrimSpace(strings.TrimPrefix(name, "FAIL "))
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// PlaywrightTitles is the title of each Playwright test a run's output says
// failed, by the name TestNamesOf gives it (`tests/e2e/x.spec.ts:106`): the last
// of the `›`-separated words of its line, without the duration or retry mark.
// A line's number changes when a branch adds a line above the test; its title
// does not (mw-gq6.967).
func PlaywrightTitles(output string) map[string]string {
	titles := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(domain.StripANSI(WithoutExpectedFailures(output)), "\r\n", "\n"), "\n") {
		m := playwrightFailure.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		words := strings.Split(playwrightTestKey(m[3]), " › ")
		if title := strings.TrimSpace(words[len(words)-1]); title != "" {
			if name := m[1] + ":" + m[2]; titles[name] == "" {
				titles[name] = title
			}
		}
	}
	return titles
}

// playwrightTestCall is a line that calls a Playwright test (`test(`,
// `test.only(`, `it.skip(`); test.describe( is a group, not a test.
var playwrightTestCall = regexp.MustCompile(`\b(?:test|it)(?:\.(?:only|skip|fixme|fail|slow))*\s*\(`)

// testCallLookback is how many lines above its title a test's call may start.
const testCallLookback = 5

// LineOfTestTitled is the line of the test titled title in a spec file's text:
// the line of its test( call, which is the line Playwright names it by, found
// from the line that holds the title. When the title is on several, the one
// nearest near; false when none holds it as a test's title.
func LineOfTestTitled(source, title string, near int) (int, bool) {
	lines := strings.Split(source, "\n")
	found, best := false, 0
	for i, text := range lines {
		if !strings.Contains(text, title) {
			continue
		}
		for j := i; j >= 0 && j >= i-testCallLookback; j-- {
			if !playwrightTestCall.MatchString(lines[j]) {
				continue
			}
			line := j + 1
			if !found || abs(line-near) < abs(best-near) {
				found, best = true, line
			}
			break
		}
	}
	return best, found
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// FailingPackages is the Go packages a run's output says failed, each once, in
// order: the import path of each `FAIL<tab><package>` line go test prints.
func FailingPackages(output string) []string {
	var packages []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "FAIL\t") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 1 && !seen[fields[1]] {
			seen[fields[1]] = true
			packages = append(packages, fields[1])
		}
	}
	return packages
}

// PackagesOutsideTheStory is the failing packages of a run's output when none of
// them holds a file the story changed (mw-gq6.861: the mail-notify tests failed
// the gate of stories that touched nothing under contrib/). It is nil when the
// output names no package, when a changed file lies in one of them, or when a
// changed file is one a build of every package reads (a Go file or module file
// at the root, the Makefile): the failure may be the story's then. A package is
// the directory a changed file is in, the tail of its import path.
func PackagesOutsideTheStory(output string, changed []string) []string {
	failing := FailingPackages(output)
	for _, file := range changed {
		dir := path.Dir(file)
		if dir == "." {
			switch base := path.Base(file); {
			case base == "go.mod", base == "go.sum", base == "go.work", base == "Makefile", strings.HasSuffix(base, ".go"):
				return nil
			}
			continue
		}
		for _, pkg := range failing {
			if pkg == dir || strings.HasSuffix(pkg, "/"+dir) {
				return nil
			}
		}
	}
	return failing
}

// MainRed is what a close-out found out about main: the tests it fails too, how
// long the finding took, and the bead that carries its fix.
type MainRed struct {
	Tests   []string
	Command string
	Took    time.Duration
	Bug     string
	NewBug  bool
}

func (m MainRed) tests() string { return strings.Join(m.Tests, ", ") }

// DefaultMainAloneBound is how long the run of the failing tests on the target
// branch may take (mw-gq6.929): the Clerk it stands in for took 27 minutes, and
// a close-out that waits as long has told the Mayor nothing sooner.
const DefaultMainAloneBound = 10 * time.Minute

// The labels a failing test is given in the note of a check of main alone
// (mw-gq6.929), one per test: it fails on main too, it fails only on the
// branch, or main was not asked, and why.
const (
	FailsOnMainToo = "fails on main too"
	BranchOnly     = "branch only"
	// NotOnMain is a Playwright test whose title the target's spec file does not
	// hold: the branch added it (mw-gq6.967).
	NotOnMain    = "not on main: the branch added it"
	NotRunPrefix = "not run: "
)

// maxLabelled is how many failing tests a note of labels lists one by one; the
// rest are counted by label.
const maxLabelled = 20

// labelNote is the note that says, of each of tests, whether it fails on main,
// by what it was checked at ("origin/main at abc1234"); label gives each test
// its label.
func labelNote(tests []string, at string, label func(test string) string) string {
	var lines []string
	counted := map[string]int{}
	var order []string
	for i, test := range tests {
		l := label(test)
		if i < maxLabelled {
			lines = append(lines, test+": "+l)
			continue
		}
		if counted[l] == 0 {
			order = append(order, l)
		}
		counted[l]++
	}
	for _, l := range order {
		lines = append(lines, fmt.Sprintf("and %d more: %s", counted[l], l))
	}
	return fmt.Sprintf("of the failing tests, run alone on %s: %s", at, strings.Join(lines, "; "))
}

// labelMain keeps note, the labels of a check of main alone, on the report: as a
// note, which the close-out's mail and ledger line carry, and as MainLabels,
// which a batch's mail has no notes to carry it by.
func labelMain(report *NextReport, note string) {
	report.Notes = append(report.Notes, note)
	report.MainLabels = note
}

// mainNotRunNote labels every test of tests "not run: <why>"; the story stops as it
// would have, since main was not asked.
func mainNotRunNote(tests []string, why string) string {
	return labelNote(tests, "main", func(string) string { return NotRunPrefix + why })
}

// waitForRoom says why the host has no room for the run on main alone, or "" when
// it has. A host over its cores is waited on first, with the gate's own wait for
// a calm host (Next.calmed, up to Next.loadBound), because main's red holds a
// story and a host that is busy by day would otherwise charge the story an
// attempt for it (mw-gq6.930); the wait says so in the output and the notes. A
// host whose load or memory cannot be read has room.
func (n Next) waitForRoom(ctx context.Context, c *closeOut, report *NextReport) string {
	reading, ok := n.readLoad(ctx)
	if !ok {
		return ""
	}
	if reading.NoRoom(RoomLimits{}) == "" {
		return ""
	}
	const what = "the run of the failing tests on main alone"
	if reading.Busy() {
		var err error
		if reading, _, err = n.calmedThen(ctx, report, c, what, "was not made", false, false); err != nil {
			// Far over its cores at the end of the bound: the host's own error
			// is the note of why, and the reading is the one it ended on.
			report.Notes = append(report.Notes, err.Error())
		}
	}
	return reading.NoRoom(RoomLimits{})
}

// mainAlone runs the tests failed (a failed run of the rig's tests) names, by
// name, on the target branch alone, in a throwaway landing worktree at base, and
// reports main's red when it fails every one of them too. Nil when it cannot be
// told, when main passes, or when main fails other tests than the story's: today's
// blame of the story then stands. The time the check took is a note of the run,
// and so on the ledger line.
//
// Whatever came of it, a note says of each failing test whether it fails on main
// too, fails on the branch only, or was not run and why (mw-gq6.929), so that the
// Mayor reads it in the mail and sends no Clerk to find out. The run is made
// only when the host has room, is cut at Next.MainAloneBound, and its worktree is
// always taken away; none of that changes what the story's stop is.
func (n Next) mainAlone(ctx context.Context, c *closeOut, report *NextReport, failed Checked, base string) *MainRed {
	if n.NamedChecks == nil {
		return nil
	}
	tests := TestNamesOf(failed.Output)
	if len(tests) == 0 {
		return nil
	}
	began := n.now()
	// The gate lock is waited for first, and the landing opened after: a host's
	// gate can queue for an hour, and the target moves meanwhile (mw-gq6.872).
	if lock := n.waitForTheGateLock(ctx, c, report, "the failing tests on main alone"); lock != nil {
		defer n.giveTheGateLockBack(report, lock)
	}
	if why := n.waitForRoom(ctx, c, report); why != "" {
		labelMain(report, mainNotRunNote(tests, n.Host+" has no room: "+why))
		return nil
	}
	at := base
	if base == StartPoint(n.remote(), c.target) {
		// The target as the remote has it now; a batch's base is a commit and
		// stays the commit its gate ran on.
		at = n.freshTarget(ctx, c, report)
	}
	dir, err := n.Landing.OpenLanding(ctx, c.rigDir, base)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("main alone was not checked for %s: a landing of %s at %s could not be opened: %v", strings.Join(tests, ", "), c.path.Rig, base, err))
		labelMain(report, mainNotRunNote(tests, fmt.Sprintf("a worktree of %s could not be opened: %s", base, firstLine(err.Error()))))
		return nil
	}
	defer func() {
		if closeErr := n.Landing.CloseLanding(ctx, c.rigDir, dir); closeErr != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the landing worktree %s could not be taken away: %v", dir, closeErr))
		}
	}()
	// A Playwright test is asked for on main at the line main has it at, which
	// a branch that added a line above it has moved (mw-gq6.967).
	onTarget, added := namesOnTarget(dir, tests, PlaywrightTitles(failed.Output))
	label := func(test, label string) string {
		if added[test] {
			return NotOnMain
		}
		return label
	}
	var asked []string
	askedSeen := map[string]bool{}
	for _, test := range tests {
		if name := onTarget[test]; !added[test] && !askedSeen[name] {
			askedSeen[name] = true
			asked = append(asked, name)
		}
	}
	if len(asked) == 0 {
		labelMain(report, labelNote(tests, at, func(string) string { return NotOnMain }))
		return nil
	}
	bound := n.MainAloneBound
	if bound <= 0 {
		bound = DefaultMainAloneBound
	}
	bounded, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	run, err := n.NamedChecks.RunNamed(bounded, c.path.Rig, dir, asked)
	took := n.now().Sub(began)
	if ctx.Err() == nil && bounded.Err() != nil {
		// Cut at its bound: what it printed so far is not an answer.
		report.Notes = append(report.Notes, fmt.Sprintf("main alone was not checked for %s: the run did not end in %s", strings.Join(tests, ", "), bound))
		labelMain(report, mainNotRunNote(tests, fmt.Sprintf("the run did not end in %s", bound)))
		return nil
	}
	switch {
	case err != nil:
		report.Notes = append(report.Notes, fmt.Sprintf("main alone was not checked for %s: %v", strings.Join(tests, ", "), err))
		labelMain(report, mainNotRunNote(tests, firstLine(err.Error())))
		return nil
	case !run.Ran:
		labelMain(report, mainNotRunNote(tests, "the rig names no command to run tests by name"))
		return nil
	case run.NotRun:
		labelMain(report, mainNotRunNote(tests, n.namedWhy(ctx, "the command to run them by name did not start", c, report, run.Output)))
		return nil
	case run.Passed:
		report.Notes = append(report.Notes, fmt.Sprintf("main alone passes %s (checked in %s): %s", strings.Join(tests, ", "), took.Round(time.Second), n.whoseFailure(ctx, c, report, failed, base)))
		labelMain(report, labelNote(tests, at, func(test string) string { return label(test, BranchOnly) }))
		return nil
	}
	onMain := map[string]bool{}
	for _, name := range TestNamesOf(run.Output) {
		onMain[name] = true
	}
	// Each test is told by the name main knows it by.
	for _, test := range tests {
		if !added[test] && onMain[onTarget[test]] {
			onMain[test] = true
		}
	}
	if len(onMain) == 0 {
		// Failed, and named no test: a build that broke, say. Nothing is said of
		// any one test.
		labelMain(report, mainNotRunNote(tests, n.namedWhy(ctx, "the run on main failed without naming a test", c, report, run.Output)))
		return nil
	}
	labelMain(report, labelNote(tests, at, func(test string) string {
		if onMain[test] {
			return FailsOnMainToo
		}
		return label(test, BranchOnly)
	}))
	for _, name := range tests {
		if !onMain[name] {
			report.Notes = append(report.Notes, fmt.Sprintf("main alone fails other tests than %s (checked in %s): %s", strings.Join(tests, ", "), took.Round(time.Second), n.whoseFailure(ctx, c, report, failed, base)))
			return nil
		}
	}
	// The bug that carries main's red names the tests by main's own lines, which
	// a branch that moved a test does not change: a later story that fails the
	// same test at another line finds the same bug (mw-gq6.967).
	onMainNames := make([]string, len(tests))
	for i, test := range tests {
		onMainNames[i] = onTarget[test]
	}
	red := &MainRed{Tests: onMainNames, Command: run.Command, Took: took}
	report.Notes = append(report.Notes, fmt.Sprintf("main alone fails %s too (checked in %s): main is red", red.tests(), took.Round(time.Second)))
	return red
}

// namesOnTarget says how each failing test is asked for on the target, in dir,
// a worktree of it: a Playwright test (one titles names) by the line its title
// is at in the target's spec file, every other name as it is. added is the
// tests whose title the target does not hold: the branch added them.
func namesOnTarget(dir string, tests []string, titles map[string]string) (names map[string]string, added map[string]bool) {
	names, added = map[string]string{}, map[string]bool{}
	for _, test := range tests {
		title := titles[test]
		file, lineText, found := strings.Cut(test, ":")
		line, err := strconv.Atoi(lineText)
		if title == "" || !found || err != nil {
			names[test] = test
			continue
		}
		var source []byte
		if filepath.IsLocal(filepath.FromSlash(file)) {
			source, _ = os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
		}
		if mainLine, there := LineOfTestTitled(string(source), title, line); there {
			names[test] = file + ":" + strconv.Itoa(mainLine)
		} else {
			names[test], added[test] = test, true
		}
	}
	return names, added
}

// MainAloneLogFileName is the file in a story's run that holds the whole output
// of a run on main alone that failed and named no test (mw-gq6.937).
const MainAloneLogFileName = "main-alone.log"

// The bounds of what a not-run label quotes of a run's output: this many of its
// last meaningful lines, each cut to this many characters.
const (
	notRunLines    = 3
	notRunLineLong = 120
)

// LastMeaningfulLines is the last lines of output that say something, up to
// lines of them, joined by " / ", each cut to long characters: a line with no
// letter or digit (a rule, a bare brace) says nothing, and nor does go test's
// bare FAIL or exit status line.
func LastMeaningfulLines(output string, lines, long int) string {
	all := strings.Split(strings.ReplaceAll(domain.StripANSI(output), "\r\n", "\n"), "\n")
	var kept []string
	for i := len(all) - 1; i >= 0 && len(kept) < lines; i-- {
		line := strings.TrimSpace(all[i])
		if !strings.ContainsFunc(line, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
			continue
		}
		if line == "FAIL" || strings.HasPrefix(line, "exit status ") {
			continue
		}
		if r := []rune(line); len(r) > long {
			line = string(r[:long]) + "..."
		}
		kept = append([]string{line}, kept...)
	}
	return strings.Join(kept, " / ")
}

// namedWhy is the label's reason for a run on main that did not give a name: a
// build that broke, a taken port, a hang, a name the parser missed. It says what
// the run printed last and keeps all of it beside the gate logs
// (runs/<story>/main-alone.log), naming the file, so that nobody has to guess
// from the command, which says nothing (mw-gq6.937).
func (n Next) namedWhy(ctx context.Context, what string, c *closeOut, report *NextReport, output string) string {
	why := what
	if last := LastMeaningfulLines(output, notRunLines, notRunLineLong); last != "" {
		why += ", it ended: " + last
	} else {
		why += " and printed nothing to say why"
	}
	if kept := n.keepMainAloneLog(ctx, c, report, output); kept != "" {
		why += " (all of it in " + kept + ")"
	}
	return why
}

// keepMainAloneLog writes the whole output of a run on main alone beside the
// story's gate logs, capped, and returns its path from the vault's root, empty
// when there is nothing to keep, no vault (mw check writes nothing anywhere) or
// the write failed, which is a note and not a reason to say less.
func (n Next) keepMainAloneLog(ctx context.Context, c *closeOut, report *NextReport, output string) string {
	if n.Vault == nil || strings.TrimSpace(output) == "" {
		return ""
	}
	name := attemptRunFileName(MainAloneLogFileName, c.attempt())
	record, err := n.putRunFile(ctx, c, name, capRunLog(name, output))
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the whole output of the run on main alone could not be kept in the run of %s: %v", c.id, err))
		return ""
	}
	if record == "" {
		return ""
	}
	c.mainLog = name
	return record
}

// whoseFailure is the end of the note a check of main alone leaves when main did
// not fail the story's tests the same way: "the failure is the story's", unless
// the packages that failed hold none of the files the story changed, which it
// says instead, so a flaky test in a package the story never touched is not put
// down to the story (mw-gq6.861).
func (n Next) whoseFailure(ctx context.Context, c *closeOut, report *NextReport, failed Checked, base string) string {
	const story = "the failure is the story's"
	if n.Changes == nil {
		return story
	}
	changed, err := n.Changes.ChangedFiles(ctx, c.rigDir, base, c.branch)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the files %s changed could not be read to tell whether the failing tests are outside them: %v", c.id, err))
		return story
	}
	if outside := PackagesOutsideTheStory(failed.Output, changed); len(outside) > 0 {
		return fmt.Sprintf("the failing tests are outside the story's files (%s holds none of the %d files it changed), so the failure may not be the story's", strings.Join(outside, ", "), len(changed))
	}
	return story
}

// carriedBy finds the open bug whose description names every failing test, or
// files a new held [bug] naming them, and records it on red. Reading the open
// stories and filing are the close-out's only writes to beads that are not the
// story's own; a bug that could not be filed leaves red.Bug empty and says so.
func (n Next) carriedBy(ctx context.Context, c *closeOut, report *NextReport, red *MainRed) {
	if lister, ok := n.Tracker.(TesterStories); ok {
		stories, err := readRetrying(ctx, n, report, "the open stories", func() ([]StoryDetail, error) { return lister.StoriesSince(ctx, time.Time{}) })
		if err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the bug that carries main's red could not be looked for, the open stories being unreadable: %v", err))
		}
	found:
		for _, d := range stories {
			if d.IsEpic || d.Closed() || d.Story.ID == c.id || (d.Merged().Rig != c.path.Rig && d.Merged().Rig != "") {
				continue
			}
			for _, name := range red.Tests {
				if !strings.Contains(d.Description, name) {
					continue found
				}
			}
			red.Bug = d.Story.ID
			return
		}
	}
	path := c.path
	path.Files, path.Bump, path.MaxTurns = "", "", ""
	title := "[bug] main is red: " + red.tests()
	story := NewStory{
		EpicID:     c.detail.EpicID,
		Standalone: c.detail.EpicID == "",
		Title:      title,
		Description: fmt.Sprintf("%s fails on %s by itself, with no story merged in: %s.\n\nFound by mw next on %s while closing out %s, whose merged result failed the same tests; "+
			"`%s` fails them too in a clean worktree of %s. Until it is fixed every landing of %s that runs the rig's tests meets the same failure, and the story is held on this bug (mw-gq6.645).\n",
			red.tests(), c.target, red.tests(), n.Host, c.id, red.Command, c.target, c.path.Rig),
		Acceptance: fmt.Sprintf("1. %s pass on %s, run by name.\n2. The rig's gate passes.\nSweep: none, because mw next found the failure by running the tests on %s alone.", red.tests(), c.target, c.target),
		Priority:   1,
		Overrides:  path,
		Labels:     []string{LabelBug},
	}
	var id string
	err := writeRetrying(ctx, n, report, "the bug that carries main's red", func() error {
		var err error
		id, err = n.Tracker.CreateStory(ctx, story)
		return err
	})
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the bug that carries main's red could not be filed: %v", err))
		n.mailLostWrite(ctx, c, report, "the bug that carries main's red", fmt.Sprintf("%s is red on %s and nothing carries its fix; file a bug naming %s: %v", c.target, c.path.Rig, red.tests(), err))
		return
	}
	red.Bug, red.NewBug = id, true
}

// holdOnMainRed is what a close-out does in place of blaming a story for a
// failure main has too: the story is held waiting on the bead that carries
// main's fix (it blocks the story), recorded as run=close-out-again with
// mainred=<bead>, and nothing is merged, pushed, sent back or counted as an
// attempt. The next dispatch tick that finds the bead closed runs the close-out
// alone again, never a fresh Builder session. The mail says "Main is red", not
// "Sent back".
//
// It holds only a story it can name the carrying bead for: when the bead could
// not be found or filed (the Mayor is mailed) the story is not held, held is
// false, and the close-out goes on as it would have.
func (n Next) holdOnMainRed(ctx context.Context, c *closeOut, report *NextReport, red *MainRed, kept string) (rep NextReport, held bool) {
	if ctx.Err() != nil {
		return NextReport{}, false
	}
	n.carriedBy(ctx, c, report, red)
	carrier := red.Bug
	if carrier == "" {
		return NextReport{}, false
	}
	report.MainRed = red
	report.Reason = ReasonMainRed
	report.Why = fmt.Sprintf("main is red: %s, carried by %s", red.tests(), carrier)

	note := fmt.Sprintf("mw next on %s did not blame %s (%s): the rig's tests fail on the merged result in %s, and `%s` fails them on %s alone too (checked in %s). Main is red.\n\n"+
		"%s carries the fix. The story is held waiting on it: its claim, worktree %s and branch %s are kept, it was not sent back, no attempt was counted, and "+
		"the next dispatch tick after %s is closed runs its close-out alone again, not a new Builder session. Nothing was pushed.",
		n.Host, c.id, ReasonMainRed, c.target, red.Command, c.target, red.Took.Round(time.Second), carrier, c.worktree, c.branch, carrier)
	if red.NewBug {
		note += fmt.Sprintf("\n\n%s was filed held by mw next, for the Mayor.", red.Bug)
	}
	if kept != "" {
		note += "\n\n" + kept
	}
	if err := writeRetrying(ctx, n, report, "the wait on main's fix", func() error { return n.Tracker.AddBlocker(ctx, c.id, red.Bug) }); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s could not be made to wait on %s: %v", c.id, red.Bug, err))
	}
	n.writeOrSay(ctx, c, report, "the main-red state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, MainRedState, red.Bug),
		func() error { return n.Tracker.SetStoryState(ctx, c.id, MainRedState, red.Bug, report.Why) })
	n.writeOrSay(ctx, c, report, "the run state", fmt.Sprintf("%s could not be recorded as %s=%s", c.id, RunState, RunCloseOutAgain),
		func() error {
			return n.Tracker.SetStoryState(ctx, c.id, RunState, RunCloseOutAgain, "("+string(ReasonMainRed)+") "+report.Why)
		})
	n.writeOrSay(ctx, c, report, "the hold on the story", "the hold could not be written on the story",
		func() error { return n.Tracker.CommentOnStory(ctx, c.id, note) })
	if err := n.ledger(ctx, c, report, NotLanded(ReasonMainRed, report.Why+"; held, close-out again once it is fixed")); err != nil {
		report.Notes = append(report.Notes, err.Error())
	}
	n.commit(ctx, c, report)
	n.mailTheMayor(ctx, c, report, MailMainRed, note)
	return *report, true
}

// mainRedWaitOver says whether a story held on main's red (MainRedState) may have
// its close-out run again: the bead that carries the fix is closed, or gone, or
// nothing was waited on. Unreadable, it is still waiting. wait is the bead.
func mainRedWaitOver(ctx context.Context, tracker WorkTracker, id string) (wait string, over bool, err error) {
	wait, err = tracker.StoryState(ctx, id, MainRedState)
	if err != nil {
		return "", false, err
	}
	if wait == "" || wait == MainRedFixed {
		return wait, true, nil
	}
	beads, err := tracker.ShowBeads(ctx, []string{wait})
	if err != nil {
		return wait, false, err
	}
	for _, d := range beads {
		if d.Story.ID == wait && !d.Closed() {
			return wait, false, nil
		}
	}
	return wait, true, nil
}

// holdBatchOnMainRed is carriedBy for a batch that went red on main's own red:
// there is no one story to hold, so the bead that carries main's red is found or
// filed, and the batch is held with its stories queued; the Held mail says so.
func (n Next) holdBatchOnMainRed(ctx context.Context, bc *closeOut, nr *NextReport, red *MainRed) bool {
	n.carriedBy(ctx, bc, nr, red)
	if red.Bug == "" {
		return false
	}
	nr.MainRed = red
	return true
}

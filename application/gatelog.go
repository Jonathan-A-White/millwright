package application

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// GateLogCap is the most a run's gate.log holds. A test run that prints more
// keeps its first GateLogHead bytes and the rest of the cap from its end, with
// a line between them that says how much was left out, so that the file is
// small enough to commit with the vault and still holds both the first
// failure and the last words.
const (
	GateLogCap  = 512 << 10
	GateLogHead = 128 << 10
)

// FailingTestsListed is the most failing tests a refusal names.
const FailingTestsListed = 20

// FailingTestsInLedger is how many of them the ledger line and the run state,
// which hold one line, name.
const FailingTestsInLedger = 3

// failingTest is a line of a test run's output that names a test that failed:
// vitest's `FAIL <file> > <test>` and its `×` lines, go test's `--- FAIL:`
// (subtests are indented), Playwright's `✘`. A trailing duration (`31ms`,
// `(0.00s)`) is left out, because the same failure printed twice differs only
// by it.
var (
	failingTest  = regexp.MustCompile(`^\s*(FAIL +\S.*|× .*|✘ .*|--- FAIL: .*)$`)
	trailingTime = regexp.MustCompile(`\s+(\(\d+(\.\d+)?m?s\)|\d+(\.\d+)?m?s)$`)
)

var (
	// playwrightRunBoundary is a line that starts a Playwright run in a log, or a
	// run of the log's own (mw's `=== <command> ===` header): the lines up to the
	// next one are one run's.
	playwrightRunBoundary = regexp.MustCompile(`^(\s*Running \d+ tests? using \d+ workers?|=== )`)
	// playwrightSummary is a count line of a finished run: `5 passed (44.6s)`,
	// `1 failed`, `2 flaky`, `1 skipped`. A run killed before it has none.
	playwrightSummary = regexp.MustCompile(`^\s*\d+ (passed|failed|flaky|skipped|did not run|interrupted)\b`)
	// playwrightListFailure is a list reporter's ✘ line: the test after the
	// number, the same words the numbered header of its detail and the list under
	// `N failed` hold.
	playwrightListFailure = regexp.MustCompile(`^\s*✘\s+\d+\s+(\[.*)$`)
	// playwrightTestWords is the `[project] › file:line:col › title` of a numbered
	// header (`1) ...`) of a failure's detail, or of a line under `N failed` or
	// `N flaky`, which have no number.
	playwrightTestWords = regexp.MustCompile(`^\s*(?:\d+\)\s+)?(\[[^\]]*\]\s+›\s+\S+\.spec\.[cm]?[jt]sx?:\d+:\d+\s+›.*)$`)
	// playwrightFileAndTitle splits a test's words into its spec file and its
	// title (the words after `file:line:col ›`).
	playwrightFileAndTitle = regexp.MustCompile(`\s(\S+\.spec\.[cm]?[jt]sx?):\d+:\d+\s+›\s*(.*)$`)
	// playwrightRetry is the `(retry #1)` Playwright adds to a test's retries.
	playwrightRetry = regexp.MustCompile(`\s*\(retry #\d+\)`)
)

// playwrightTestKey is the words that say which test a Playwright line is
// about, without the duration, the retry mark or the rule of box characters a
// header is padded with.
func playwrightTestKey(words string) string {
	words = trailingTime.ReplaceAllString(strings.TrimSpace(words), "")
	words = playwrightRetry.ReplaceAllString(words, "")
	return strings.Join(strings.Fields(strings.TrimRight(strings.TrimSpace(words), "─ ")), " ")
}

// playwrightFailureKey is what a failing-test line is told apart by: for a
// Playwright ✘ line the test file and title, without the ✘ index, the project,
// the line and column, the duration or the retry mark; any other line as it is.
func playwrightFailureKey(name string) string {
	m := playwrightListFailure.FindStringSubmatch(name)
	if m == nil {
		return name
	}
	words := playwrightTestKey(m[1])
	if f := playwrightFileAndTitle.FindStringSubmatch(words); f != nil {
		return "✘ " + f[1] + " › " + strings.TrimSpace(f[2])
	}
	return name
}

// WithoutExpectedFailures is a run's output with the ✘ lines of Playwright's
// expected failures taken out (mw-gq6.958). A test marked test.fail() that fails
// as it should is drawn by the list reporter with a ✘, though the run counts it
// passed and exits 0; mw read every ✘ as a failing test, and named the file
// failing in a whole run that passed alone. What tells the two apart is the
// rest of a finished run: a test that failed has its numbered detail and a line
// under `N failed` (`N flaky` for one that passed on a retry), and an expected
// failure has neither. A ✘ line stays when its run has no summary (it was cut
// off, and cannot be told), and one that the detail or the failed list names.
func WithoutExpectedFailures(output string) string {
	if !strings.Contains(output, "✘") {
		return output
	}
	lines := strings.Split(strings.ReplaceAll(domain.StripANSI(output), "\r\n", "\n"), "\n")
	drop := make([]bool, len(lines))
	for start := 0; start < len(lines); {
		end := start + 1
		for end < len(lines) && !playwrightRunBoundary.MatchString(lines[end]) {
			end++
		}
		dropExpectedFailures(lines[start:end], drop[start:end])
		start = end
	}
	var kept []string
	for i, line := range lines {
		if !drop[i] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// dropExpectedFailures marks, in the lines of one run, the ✘ lines that a
// finished run does not name as failed.
func dropExpectedFailures(lines []string, drop []bool) {
	finished, saysFailed := false, false
	var failed []string
	for _, line := range lines {
		if m := playwrightSummary.FindStringSubmatch(line); m != nil {
			finished = true
			saysFailed = saysFailed || m[1] == "failed"
		}
		if strings.HasPrefix(line, "[... ") && strings.Contains(line, "left out by mw") {
			return // the middle of the log was cut: what is left cannot be told apart
		}
		if m := playwrightTestWords.FindStringSubmatch(line); m != nil {
			failed = append(failed, playwrightTestKey(m[1]))
		}
	}
	// A run that says some failed, and names none of them, is a log whose detail
	// is not here: every ✘ stands.
	if !finished || (saysFailed && len(failed) == 0) {
		return
	}
	for i, line := range lines {
		m := playwrightListFailure.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := playwrightTestKey(m[1])
		drop[i] = !slices.ContainsFunc(failed, func(f string) bool {
			return strings.HasPrefix(f, key) || strings.HasPrefix(key, f)
		})
	}
}

// FailingTests is the tests a run's output says failed, in the order it said
// them, each once, at most limit of them; more is how many past the limit
// there were.
func FailingTests(output string, limit int) (names []string, more int) {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(domain.StripANSI(WithoutExpectedFailures(output)), "\r\n", "\n"), "\n") {
		if !failingTest.MatchString(line) {
			continue
		}
		name := strings.Join(strings.Fields(trailingTime.ReplaceAllString(strings.TrimSpace(line), "")), " ")
		// A Playwright test is one test by its file and title: its ✘ index, its
		// line and its retry mark differ between the lines that name it
		// (mw-gq6.970). The line shown stays the first one, as it always was.
		key := playwrightFailureKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(names) < limit {
			names = append(names, name)
		} else {
			more++
		}
	}
	return names, more
}

// FailingTestsBlock is the failing tests of a run as a list for a mail or a
// comment, empty when the output names none.
func FailingTestsBlock(output string) string {
	names, more := FailingTests(output, FailingTestsListed)
	if len(names) == 0 {
		return ""
	}
	listed := len(names) + more
	lines := []string{fmt.Sprintf("Failing tests (%d):", listed)}
	for _, name := range names {
		lines = append(lines, "- "+name)
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf("- and %d more", more))
	}
	return strings.Join(lines, "\n")
}

// FailingTestsShort is the first few failing tests of a run on one line, for
// the ledger and the run state; empty when the output names none.
func FailingTestsShort(output string) string {
	names, more := FailingTests(output, FailingTestsInLedger)
	if len(names) == 0 {
		return ""
	}
	return "failing: " + strings.Join(names, "; ") + moreWords(more)
}

func moreWords(more int) string {
	if more == 0 {
		return ""
	}
	return fmt.Sprintf(" (and %d more)", more)
}

// CapGateLog is output cut to GateLogCap bytes: the head and the tail kept,
// the middle replaced by a line that says how many bytes it was.
func CapGateLog(output string) string { return capRunLog(GateLogFileName, output) }

// capRunLog is output cut as CapGateLog cuts it, for the run file name, which
// the line between head and tail names.
func capRunLog(name, output string) string {
	output = domain.StripANSI(output)
	if len(output) <= GateLogCap {
		return output
	}
	tail := GateLogCap - GateLogHead
	left := len(output) - GateLogHead - tail
	return strings.ToValidUTF8(output[:GateLogHead], "") +
		fmt.Sprintf("\n[... %d bytes left out by mw: %s keeps the first %d and the last %d ...]\n", left, name, GateLogHead, tail) +
		strings.ToValidUTF8(output[len(output)-tail:], "")
}

// keepGateLog writes the whole output of a failed test run beside the story's
// run, capped, and returns the sentence that says where it is, empty when it
// could not be written (a note, not a reason to say less) or when there is no
// vault to write it in, as in mw check, which writes nothing anywhere.
func (n Next) keepGateLog(ctx context.Context, c *closeOut, report *NextReport, checked Checked) string {
	if n.Vault == nil {
		return ""
	}
	name := GateLogFileNameForAttempt(c.attempt())
	kept, err := n.putRunFile(ctx, c, name, CapGateLog(checked.Output))
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the whole output of the tests could not be kept in the run of %s: %v", c.id, err))
		return ""
	}
	c.gateLog = name
	if c.place != nil {
		return "The whole output of the tests is kept at " + kept + "."
	}
	return "The whole output of the tests is kept at " + n.Vault.RunFile(c.id, name) + "."
}

// failedTestsDetail is what a refusal for failing tests says beside its reason:
// the tests by name first, then the last lines of the run, then where the whole
// of it is kept.
func (n Next) failedTestsDetail(ctx context.Context, c *closeOut, report *NextReport, checked Checked, where string) string {
	var detail []string
	if block := FailingTestsBlock(checked.Output); block != "" {
		detail = append(detail, block)
	}
	detail = append(detail, "The last lines of `"+checked.Command+"` in "+where+":\n\n"+fenced(checked.Tail(CheckLines)))
	if kept := n.keepGateLog(ctx, c, report, checked); kept != "" {
		detail = append(detail, kept)
	}
	return strings.Join(detail, "\n\n")
}

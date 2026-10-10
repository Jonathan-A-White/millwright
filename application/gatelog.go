package application

import (
	"context"
	"fmt"
	"regexp"
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

// FailingTests is the tests a run's output says failed, in the order it said
// them, each once, at most limit of them; more is how many past the limit
// there were.
func FailingTests(output string, limit int) (names []string, more int) {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(domain.StripANSI(output), "\r\n", "\n"), "\n") {
		if !failingTest.MatchString(line) {
			continue
		}
		name := strings.Join(strings.Fields(trailingTime.ReplaceAllString(strings.TrimSpace(line), "")), " ")
		if seen[name] {
			continue
		}
		seen[name] = true
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
func CapGateLog(output string) string {
	output = domain.StripANSI(output)
	if len(output) <= GateLogCap {
		return output
	}
	tail := GateLogCap - GateLogHead
	left := len(output) - GateLogHead - tail
	return strings.ToValidUTF8(output[:GateLogHead], "") +
		fmt.Sprintf("\n[... %d bytes left out by mw: gate.log keeps the first %d and the last %d ...]\n", left, GateLogHead, tail) +
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
	if _, err := n.Vault.PutRunFile(ctx, c.id, name, CapGateLog(checked.Output)); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the whole output of the tests could not be kept in the run of %s: %v", c.id, err))
		return ""
	}
	c.gateLog = name
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

package application_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestFailingTestsReadsEveryRunnersWords(t *testing.T) {
	output := strings.Join([]string{
		" FAIL  src/review.test.tsx > Review > shows the due count",
		" × Review > shows the due count 31ms",
		" ✓ Review > shows the title 4ms",
		"--- FAIL: TestLedger (0.00s)",
		"    --- FAIL: TestLedger/second (0.00s)",
		"FAIL\tgithub.com/Jonathan-A-White/millwright/application\t0.012s",
		"FAIL",
		"  ✘  1 [chromium] › home.spec.ts:3:1 › opens (1.2s)",
		" FAIL  src/review.test.tsx > Review > shows the due count",
		"\x1b[31m × \x1b[39mcoloured 2ms",
	}, "\n")
	names, more := application.FailingTests(output, 20)
	want := []string{
		"FAIL src/review.test.tsx > Review > shows the due count",
		"× Review > shows the due count",
		"--- FAIL: TestLedger",
		"--- FAIL: TestLedger/second",
		"✘ 1 [chromium] › home.spec.ts:3:1 › opens",
		"× coloured",
	}
	if more != 0 || strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Errorf("FailingTests = %q (+%d more), want %q", names, more, want)
	}
}

func TestFailingTestsListsAtMostTheLimitAndCountsTheRest(t *testing.T) {
	var out strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&out, "--- FAIL: TestN%d (0.00s)\n", i)
	}
	names, more := application.FailingTests(out.String(), application.FailingTestsListed)
	if len(names) != 20 || more != 5 {
		t.Errorf("got %d names and %d more, want 20 and 5", len(names), more)
	}
	block := application.FailingTestsBlock(out.String())
	if !strings.HasPrefix(block, "Failing tests (25):\n- --- FAIL: TestN0\n") || !strings.HasSuffix(block, "\n- and 5 more") {
		t.Errorf("block = %q", block)
	}
	if short := application.FailingTestsShort(out.String()); short != "failing: --- FAIL: TestN0; --- FAIL: TestN1; --- FAIL: TestN2 (and 22 more)" {
		t.Errorf("short = %q", short)
	}
}

func TestOutputThatNamesNoTestGivesNoFailingTests(t *testing.T) {
	if got := application.FailingTestsBlock("undefined: Ledger\n"); got != "" {
		t.Errorf("block = %q, want none", got)
	}
	if got := application.FailingTestsShort("undefined: Ledger\n"); got != "" {
		t.Errorf("short = %q, want none", got)
	}
}

func TestAGateLogOverTheCapKeepsItsHeadAndItsTail(t *testing.T) {
	small := "short output\n"
	if got := application.CapGateLog(small); got != small {
		t.Errorf("a small output was changed: %q", got)
	}
	big := "HEAD" + strings.Repeat("x", application.GateLogCap*2) + "TAIL"
	got := application.CapGateLog(big)
	if len(got) > application.GateLogCap+200 {
		t.Errorf("the log is %d bytes, over its cap", len(got))
	}
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || !strings.Contains(got, "bytes left out by mw") {
		t.Errorf("the head, the tail and the note were not all kept")
	}
}

package application_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// keyboardSweepRun is the real output of runs/suite/lampas/2026-11-03.log
// (lines 35283-35291): Playwright's list reporter draws a test.fail() test that
// fails as expected with a ✘, and the run says 5 passed and exits 0.
const keyboardSweepRun = `Running 5 tests using 1 worker

  ✓  1 [shots] › tests/e2e/keyboard-typing-sweep.spec.ts:87:3 › Ask for a book (Library): with the keyboard up its field and its submit can be reached and nothing sits on them (1.9s)
  ✓  2 [shots] › tests/e2e/keyboard-typing-sweep.spec.ts:87:3 › Ask for another approach (Settings): with the keyboard up its field and its submit can be reached and nothing sits on them (3.8s)
  ✘  3 [shots] › tests/e2e/keyboard-typing-sweep.spec.ts:87:3 › Ask for more (Settings): with the keyboard up its field and its submit can be reached and nothing sits on them (10.7s)
  ✘  4 [shots] › tests/e2e/keyboard-typing-sweep.spec.ts:87:3 › About you (Settings): with the keyboard up its field and its submit can be reached and nothing sits on them (9.6s)
  ✓  5 [shots] › tests/e2e/keyboard-typing-sweep.spec.ts:87:3 › Summary (Verse view, Practise): with the keyboard up its field and its submit can be reached and nothing sits on them (14.3s)

  5 passed (44.6s)
`

// A run that holds an expected failure and a real one: the real one prints its
// detail and is listed under "1 failed"; the expected one is in neither.
const expectedAndReal = `Running 4 tests using 1 worker

  ✘  1 [shots] › tests/e2e/passage-typography.spec.ts:85:3 › Immersive on: the english passage is set as the Reader sets it (2.9s)
  ✓  2 [shots] › tests/e2e/passage-typography.spec.ts:85:3 › Immersive on: the greek passage is set as the Reader sets it (2.8s)
  ✘  3 [shots] › tests/e2e/keyboard-typing-sweep.spec.ts:87:3 › About you (Settings): with the keyboard up its field and its submit can be reached and nothing sits on them (9.6s)
  ✓  4 [shots] › tests/e2e/passage-typography.spec.ts:96:1 › Immersive off: the English passage is set as the Reader sets it (2.4s)


  1) [shots] › tests/e2e/passage-typography.spec.ts:85:3 › Immersive on: the english passage is set as the Reader sets it 

    Error: expect(received).toBe(expected) // Object.is equality

    Expected: "justify"
    Received: "start"

  1 failed
    [shots] › tests/e2e/passage-typography.spec.ts:85:3 › Immersive on: the english passage is set as the Reader sets it 
  3 passed (3.3m)
`

func TestAPlaywrightExpectedFailureIsNoFailingTest(t *testing.T) {
	if got := application.FailingTestsBlock(keyboardSweepRun); got != "" {
		t.Errorf("FailingTestsBlock = %q, want none: the run says 5 passed", got)
	}
	if got := application.TestNamesOf(keyboardSweepRun); len(got) != 0 {
		t.Errorf("TestNamesOf = %q, want none", got)
	}
	if got := application.FailingFiles(keyboardSweepRun); len(got) != 0 {
		t.Errorf("FailingFiles = %+v, want none", got)
	}
	if got := application.FailingTestFiles(keyboardSweepRun); len(got) != 0 {
		t.Errorf("FailingTestFiles = %q, want none", got)
	}
}

func TestARealFailureBesideAnExpectedOneIsStillNamed(t *testing.T) {
	names, _ := application.FailingTests(expectedAndReal, 20)
	if len(names) != 1 || !strings.Contains(names[0], "passage-typography.spec.ts:85:3") {
		t.Errorf("FailingTests = %q, want only the passage-typography test", names)
	}
	if got, want := application.TestNamesOf(expectedAndReal), []string{"tests/e2e/passage-typography.spec.ts:85"}; !reflect.DeepEqual(got, want) {
		t.Errorf("TestNamesOf = %q, want %q", got, want)
	}
	files := application.FailingFiles(expectedAndReal)
	if len(files) != 1 || files[0].File != "tests/e2e/passage-typography.spec.ts" {
		t.Errorf("FailingFiles = %+v, want only passage-typography", files)
	}
	if got, want := application.FailingTestFiles(expectedAndReal), []string{"tests/e2e/passage-typography.spec.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("FailingTestFiles = %q, want %q", got, want)
	}
}

// Two runs in one log: a test expected to fail in the first is a real failure in
// the second, and a run that was cut off before its summary cannot be told apart,
// so its ✘ lines stand.
func TestEachPlaywrightRunIsReadByItsOwnSummary(t *testing.T) {
	cut := "Running 2 tests using 1 worker\n\n  ✘  1 [shots] › tests/e2e/c.spec.ts:3:1 › cut off (1.0s)\n"
	got, _ := application.FailingTests(keyboardSweepRun+"\n=== next ===\n"+cut, 20)
	if len(got) != 1 || !strings.Contains(got[0], "c.spec.ts") {
		t.Errorf("FailingTests = %q, want only the run with no summary", got)
	}
	both := keyboardSweepRun + "\n" + expectedAndReal
	if names, _ := application.FailingTests(both, 20); len(names) != 1 {
		t.Errorf("FailingTests of two runs = %q, want one", names)
	}
}

// A log whose detail is not there (it says 1 failed and names none, or its middle
// was left out by mw) cannot be told apart, so every ✘ stands.
func TestPlaywrightLinesThatCannotBeToldApartAreAllKept(t *testing.T) {
	named := "Running 2 tests using 1 worker\n  ✘  1 [shots] › tests/e2e/a.spec.ts:3:1 › one (1.0s)\n  1 failed\n  1 passed (3s)\n"
	if names, _ := application.FailingTests(named, 20); len(names) != 1 {
		t.Errorf("a failed count with no failed test named: %q, want the ✘ line kept", names)
	}
	cap := "Running 2 tests using 1 worker\n  ✘  1 [shots] › tests/e2e/a.spec.ts:3:1 › one (1.0s)\n" +
		"[... 99 bytes left out by mw: gate.log keeps the first 1 and the last 2 ...]\n  2 passed (3s)\n"
	if names, _ := application.FailingTests(cap, 20); len(names) != 1 {
		t.Errorf("a log cut in the middle: %q, want the ✘ line kept", names)
	}
}

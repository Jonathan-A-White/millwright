package application_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestFailingFilesNamesEachFileOnceWithItsFirstTestAndError(t *testing.T) {
	vitest := "\x1b[31m FAIL \x1b[39m  src/dates.test.ts > parses dates > in UTC\n" +
		"AssertionError: expected 1 to be 2\n" +
		" ❯ src/dates.test.ts:12:5\n\n" +
		" FAIL  src/dates.test.ts > formats dates\n" +
		"Error: later\n" +
		" FAIL  src/config.test.ts\n" +
		"Error: suite failed to run\n"
	got := application.FailingFiles(vitest)
	want := []application.FailedFile{
		{File: "src/dates.test.ts", Test: "parses dates > in UTC", Error: "AssertionError: expected 1 to be 2\n❯ src/dates.test.ts:12:5"},
		{File: "src/config.test.ts", Error: "Error: suite failed to run"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vitest: got %+v\nwant %+v", got, want)
	}

	playwright := "Running 3 tests using 1 worker\n" +
		"  ✓  1 [shots] › tests/e2e/a.spec.ts:3:1 › fine (1.0s)\n" +
		"  ✘  2 [shots] › tests/e2e/immersive-reader.spec.ts:44:5 › shows the group (2.1s)\n\n\n" +
		"  1) [shots] › tests/e2e/immersive-reader.spec.ts:44:5 › shows the group ──────\n\n" +
		"    Error: expect(locator).toBeVisible()\n\n" +
		"    Locator: getByText('x')\n"
	got = application.FailingFiles(playwright)
	want = []application.FailedFile{{
		File:  "tests/e2e/immersive-reader.spec.ts",
		Test:  "shows the group",
		Error: "Error: expect(locator).toBeVisible()\nLocator: getByText('x')",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("playwright: got %+v\nwant %+v", got, want)
	}

	// The line and dot reporters print no progress lines, only the numbered headers.
	got = application.FailingFiles("  1) [shots] › tests/e2e/b.spec.ts:9:1 › the title ──────\n\n    Error: nope\n")
	want = []application.FailedFile{{File: "tests/e2e/b.spec.ts", Test: "the title", Error: "Error: nope"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("playwright headers: got %+v\nwant %+v", got, want)
	}

	if got := application.FailingFiles("--- FAIL: TestX (0.00s)\nFAIL\n"); len(got) != 0 {
		t.Errorf("a go test failure names no file, got %+v", got)
	}
}

package application

import (
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// SuiteErrorLines is the most lines of a failing file's error a suite keeps,
// and SuiteErrorWidth the longest it keeps of one.
const (
	SuiteErrorLines = 6
	SuiteErrorWidth = 200
)

var (
	// testFileName is a test file inside a line of a test run's output:
	// vitest's `src/a.test.ts`, Playwright's `tests/e2e/a.spec.ts` (its
	// `:44:5` is not part of the name) and go's `a_test.go`.
	testFileName = regexp.MustCompile(`[A-Za-z0-9_@./\-]+(?:\.(?:test|spec)\.[cm]?[jt]sx?|_test\.go)`)
	// errorHeader is the line a run's error detail follows: vitest's
	// `FAIL <file> > <test>` and Playwright's `1) [project] › <file>:<line> ›`.
	errorHeader = regexp.MustCompile(`^\s*(FAIL +\S|\d+\) )`)
	atLine      = regexp.MustCompile(`^:\d+(:\d+)?`)
	// numberedHeader is Playwright's `1) [project] › <file>:<line> › <test>`.
	numberedHeader = regexp.MustCompile(`^\s*\d+\) `)
)

// FailedFile is a test file a run's output says failed: its first failing test
// as the output names it (empty when the output names none, as for a file that
// failed to load) and the first lines of its error.
type FailedFile struct {
	File  string
	Test  string
	Error string
}

// FailingFiles is the test files a run's output says failed, each once, in the
// order the output said them. It reads the same lines FailingTests does — the
// failing test lines of vitest, Playwright and go test — and keeps those that
// name a test file; a failure whose output names none (go test's, which names
// packages) comes back as no file.
func FailingFiles(output string) []FailedFile {
	lines := strings.Split(strings.ReplaceAll(domain.StripANSI(output), "\r\n", "\n"), "\n")
	var files []FailedFile
	seen := map[string]bool{}
	for _, line := range lines {
		// A Playwright run that prints no progress lines (the line or dot
		// reporter) names its failures only by the numbered headers of its detail.
		if !failingTest.MatchString(line) && !numberedHeader.MatchString(line) {
			continue
		}
		file := testFileName.FindString(line)
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, FailedFile{File: file, Test: testAfter(line, file), Error: errorOf(lines, file)})
	}
	return files
}

// testAfter is the part of a failing test line that follows the file: the
// test's name, without the line and column Playwright adds or the duration.
func testAfter(line, file string) string {
	rest := line[strings.Index(line, file)+len(file):]
	rest = strings.Join(strings.Fields(trailingTime.ReplaceAllString(strings.TrimSpace(rest), "")), " ")
	rest = atLine.ReplaceAllString(rest, "")
	rest = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), ">›"))
	// Playwright draws a rule of box characters after a header's test name.
	return strings.TrimSpace(strings.TrimRight(rest, "─ "))
}

// errorOf is the first lines that follow the first error header naming file:
// the blank ones left out, up to the next failing test line.
func errorOf(lines []string, file string) string {
	for i, line := range lines {
		if !errorHeader.MatchString(line) || !strings.Contains(line, file) {
			continue
		}
		var kept []string
		for _, next := range lines[i+1:] {
			if failingTest.MatchString(next) || errorHeader.MatchString(next) || len(kept) == SuiteErrorLines {
				break
			}
			next = strings.TrimSpace(next)
			if next == "" {
				continue
			}
			if len(next) > SuiteErrorWidth {
				next = strings.ToValidUTF8(next[:SuiteErrorWidth], "") + "..."
			}
			kept = append(kept, next)
		}
		return strings.Join(kept, "\n")
	}
	return ""
}

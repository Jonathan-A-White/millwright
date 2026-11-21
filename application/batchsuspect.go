package application

import (
	"context"
	"strings"
)

// findFailingOwner looks, for a batch held after its tests went red, for the one
// story whose branch alone changed the files of the tests that failed, and
// notes it on the report (failingOwner) for the batch's line to carry as its
// suspect (mw-6ww.119.11.1.2). It is a guess from the run's own output: a
// failing test that names no file, a file two branches changed, or a branch
// that cannot be read, leaves no suspect, and the stories are landed in halves
// instead. It reads git and writes nothing.
func (b BatchLand) findFailingOwner(ctx context.Context, rigDir string, report *BatchReport) {
	if report.RedAt.IsZero() || len(report.Merged) < 2 || report.Gate.Passed || b.Next.Changes == nil {
		return
	}
	text := failureText(report.Gate.Output)
	if text == "" {
		return
	}
	owners := map[string][]string{} // a test file of the failure -> the stories that changed it
	for _, s := range report.Merged {
		files, err := b.Next.Changes.ChangedFiles(ctx, rigDir, report.Base, s.Branch)
		if err != nil {
			return
		}
		for _, file := range files {
			if isTestFile(file) && namedIn(text, file) {
				owners[file] = append(owners[file], s.ID)
			}
		}
	}
	var only string
	for _, ids := range owners {
		if len(ids) != 1 || only != "" && only != ids[0] {
			return
		}
		only = ids[0]
	}
	report.failingOwner = only
}

// failureText is the lines of a test run's output that say a test failed, each
// with the few indented lines under it (where go test names the file and line
// of the failure): the text a failing test's file is looked for in. A file that
// only passing tests name is not in it.
func failureText(output string) string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	var kept []string
	for i, line := range lines {
		if names, _ := FailingTests(line, 1); len(names) == 0 {
			continue
		}
		kept = append(kept, line)
		for _, next := range lines[i+1 : min(i+5, len(lines))] {
			if next == "" || next[0] != ' ' && next[0] != '\t' || isTestResultLine(next) {
				break
			}
			kept = append(kept, next)
		}
	}
	return strings.Join(kept, "\n")
}

// isTestResultLine says a line is the result of a test of its own, which ends
// the lines kept under a failure: a passing test's file is not a failing one's.
func isTestResultLine(line string) bool {
	line = strings.TrimSpace(line)
	for _, mark := range []string{"✓", "✔", "✘", "×", "---", "===", "FAIL", "PASS", "ok ", "-  "} {
		if strings.HasPrefix(line, mark) {
			return true
		}
	}
	return false
}

// isTestFile says a path is one a test lives in, by the names the rigs' own
// runners use: _test.go, .test.ts, .spec.ts, a test or e2e directory, a
// feature file.
func isTestFile(path string) bool {
	p := strings.ToLower(path)
	for _, mark := range []string{"_test.", ".test.", ".spec.", "/test/", "/tests/", "/e2e/", ".feature"} {
		if strings.Contains(p, mark) {
			return true
		}
	}
	return strings.HasPrefix(p, "test/") || strings.HasPrefix(p, "tests/") || strings.HasPrefix(p, "e2e/")
}

// namedIn says text names the file, by its path or by its base name.
func namedIn(text, file string) bool {
	if strings.Contains(text, file) {
		return true
	}
	base := file[strings.LastIndex(file, "/")+1:]
	return len(base) > 0 && strings.Contains(text, base)
}

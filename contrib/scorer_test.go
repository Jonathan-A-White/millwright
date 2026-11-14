package contrib_test

// The scorer's own tests are Python, in contrib/scorer/tests, run by
// contrib/scorer/test.sh. The gate never needs torch: it runs the tests that
// need no model whenever python3 is here, and every test, the three fixture
// clips included, only on a host where contrib/scorer/install.sh has made the
// venv; elsewhere that one skips, and `make test` ends with test.sh --gate-note,
// which says so in the gate's own output (mw-gq6.644: the full run once failed
// and the gate, which had skipped it, said nothing).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestScorerTestsThatNeedNoModel(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	// About 40 s on an idle host, so the 60 s default killed it on a loaded one.
	runScorerTests(t, scorerPureDeadline, "--pure")
}

func TestScorerScoresTheFixtureClips(t *testing.T) {
	t.Parallel()
	home := os.Getenv("MW_SCORER_HOME")
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home directory")
		}
		home = filepath.Join(dir, ".local", "share", "mw-scorer")
	}
	if _, err := os.Stat(filepath.Join(home, "venv", "bin", "python")); err != nil {
		t.Skipf("no scorer venv at %s: contrib/scorer/install.sh has not run on this host", filepath.Join(home, "venv"))
	}
	readScorerSources(t)
	runScorerTests(t, scorerFullDeadline)
}

const (
	// scorerPureDeadline is for the tests that need no model: about 25 to 40 s
	// on an idle host.
	scorerPureDeadline = 5 * time.Minute
	// scorerFullDeadline is for the one run that loads the real model: about
	// 105 s at load 20 of 20 cores, and the first run of mw-gq6.1257 failed at
	// load 77 of 20 (mw-gq6.1295), where the work takes several times as long.
	// It stays under the 20 minute package timeout of `make test`, and a run
	// that does not end by it is cut with the test it was in named.
	scorerFullDeadline = 15 * time.Minute
)

// runScorerTests runs contrib/scorer/test.sh with -v, so that a run cut at its
// deadline still shows which test it was in, and fails the test with
// scorerFailure.
func runScorerTests(t *testing.T, deadline time.Duration, args ...string) {
	t.Helper()
	args = append(append([]string{}, args...), "-v")
	out, err := commandWithin(t, deadline, "scorer/test.sh", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("contrib/scorer/test.sh %s: %s\n\nthe whole output:\n%s", strings.Join(args, " "), scorerFailure(string(out), err, deadline), out)
	}
}

var (
	// unittest -v prints "test_x (mod.Class.test_x) ... " when a test starts and
	// the result after it, on the same line.
	scorerStartedTest = regexp.MustCompile(`(?m)^(test\S* \([^)\n]*\)) \.\.\. ?$`)
	// ... and a "FAIL: " or "ERROR: " line for each that failed, in its summary.
	scorerFailedTest = regexp.MustCompile(`(?m)^(?:FAIL|ERROR): (test\S* \([^)\n]*\))`)
)

// scorerFailure says what failed in a run of test.sh -v that ended in err: each
// scorer test that failed or errored, or, when the run was cut with no result
// in it (the deadline), the test it was in.
func scorerFailure(out string, err error, deadline time.Duration) string {
	var failed []string
	seen := map[string]bool{}
	for _, m := range scorerFailedTest.FindAllStringSubmatch(out, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			failed = append(failed, m[1])
		}
	}
	if len(failed) > 0 {
		return fmt.Sprintf("%v; failed: %s", err, strings.Join(failed, ", "))
	}
	if m := scorerStartedTest.FindAllStringSubmatch(out, -1); len(m) > 0 {
		return fmt.Sprintf("%v: no result after %v; it was in %s", err, deadline, m[len(m)-1][1])
	}
	return fmt.Sprintf("%v: no test had started (the run did not get as far as the first test)", err)
}

// readScorerSources reads every file of the scorer from this process, because go
// test keys its cache on the files the test binary itself opens: test.sh's Python
// is read only by the child, so without this an edit to the scorer or its tests
// left the last passing result standing (mw-gq6.644).
func readScorerSources(t *testing.T) {
	t.Helper()
	err := filepath.WalkDir("scorer", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		_, err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// scorerScript runs test.sh with the scorer's home and a PATH given, and returns
// what it printed.
func scorerScript(t *testing.T, home, bin string, args ...string) (string, error) {
	t.Helper()
	cmd := command(t, "scorer/test.sh", args...)
	cmd.Env = append(os.Environ(), "MW_SCORER_HOME="+home, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// standInVenv makes a scorer home whose venv python only writes its arguments to
// a file, and a bin directory holding a stand-in espeak-ng.
func standInVenv(t *testing.T) (home, bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	home, bin, argsFile = filepath.Join(dir, "home"), filepath.Join(dir, "bin"), filepath.Join(dir, "args")
	for _, d := range []string{filepath.Join(home, "venv", "bin"), bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	python := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	for path, body := range map[string]string{filepath.Join(home, "venv", "bin", "python"): python, filepath.Join(bin, "espeak-ng"): "#!/bin/sh\n"} {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, bin, argsFile
}

func TestScorerGateSaysSoWhenTheFullTestsCannotRun(t *testing.T) {
	t.Parallel()
	out, err := scorerScript(t, filepath.Join(t.TempDir(), "no-home"), t.TempDir(), "--gate-note")
	if err != nil {
		t.Fatalf("--gate-note must not fail the gate: %v\n%s", err, out)
	}
	for _, want := range []string{"NOT RUN", "no scorer venv", "--pure"} {
		if !strings.Contains(out, want) {
			t.Errorf("the note does not say %q:\n%s", want, out)
		}
	}
}

func TestScorerGateIsSilentAndRunsEverythingWhereTheVenvIs(t *testing.T) {
	t.Parallel()
	home, bin, argsFile := standInVenv(t)

	out, err := scorerScript(t, home, bin, "--gate-note")
	if err != nil || out != "" {
		t.Fatalf("--gate-note where the full run can run: want silence, got err %v, output:\n%s", err, out)
	}

	if out, err := scorerScript(t, home, bin); err != nil {
		t.Fatalf("test.sh: %v\n%s", err, out)
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "-p test_*.py") || strings.Contains(string(got), "test_[as]*.py") {
		t.Errorf("test.sh did not run every test file: python was called with %q", got)
	}
}

func TestScorerFailureNamesTheTestThatWasRunningWhenTheDeadlineCutIt(t *testing.T) {
	t.Parallel()
	out := "test_a_clip (tests.test_fixtures.Fixtures.test_a_clip) ... ok\n" +
		"test_the_slow_clip (tests.test_fixtures.Fixtures.test_the_slow_clip) ... "
	got := scorerFailure(out, errors.New("signal: killed"), 8*time.Minute)
	for _, want := range []string{"8m0s", "test_the_slow_clip (tests.test_fixtures.Fixtures.test_the_slow_clip)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the failure does not say %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "test_a_clip") {
		t.Errorf("the failure names a test that had finished:\n%s", got)
	}
}

func TestScorerFailureNamesTheTestsThatFailed(t *testing.T) {
	t.Parallel()
	out := "test_ok (tests.test_fixtures.Fixtures.test_ok) ... ok\n" +
		"test_bad_clip (tests.test_fixtures.Fixtures.test_bad_clip) ... FAIL\n" +
		"test_broken (tests.test_align.Align.test_broken) ... ERROR\n\n" +
		"======================================================================\n" +
		"FAIL: test_bad_clip (tests.test_fixtures.Fixtures.test_bad_clip)\n" +
		"----------------------------------------------------------------------\n" +
		"AssertionError: 3 != 4\n\n" +
		"======================================================================\n" +
		"ERROR: test_broken (tests.test_align.Align.test_broken)\n" +
		"----------------------------------------------------------------------\n" +
		"KeyError: 'x'\n"
	got := scorerFailure(out, errors.New("exit status 1"), 8*time.Minute)
	for _, want := range []string{"test_bad_clip (tests.test_fixtures.Fixtures.test_bad_clip)", "test_broken (tests.test_align.Align.test_broken)"} {
		if !strings.Contains(got, want) {
			t.Errorf("the failure does not name %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "test_ok") {
		t.Errorf("the failure names a test that passed:\n%s", got)
	}
}

func TestScorerFailureOfASetupThatNeverNamedATestSaysSo(t *testing.T) {
	t.Parallel()
	got := scorerFailure("test.sh: no scorer venv\n", errors.New("exit status 1"), time.Minute)
	if !strings.Contains(got, "no test had started") {
		t.Errorf("want the failure to say no test had started:\n%s", got)
	}
}

func TestScorerGivesVerboseToPythonSoAKilledRunNamesItsTest(t *testing.T) {
	t.Parallel()
	home, bin, argsFile := standInVenv(t)
	for _, args := range [][]string{{"-v"}} {
		if out, err := scorerScript(t, home, bin, args...); err != nil {
			t.Fatalf("test.sh %v: %v\n%s", args, err, out)
		}
		got, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), " -v") && !strings.HasPrefix(string(got), "-v") {
			t.Errorf("test.sh %v did not give -v to python: %q", args, got)
		}
	}
}

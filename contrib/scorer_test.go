package contrib_test

// The scorer's own tests are Python, in contrib/scorer/tests, run by
// contrib/scorer/test.sh. The gate never needs torch: it runs the tests that
// need no model whenever python3 is here, and every test, the three fixture
// clips included, only on a host where contrib/scorer/install.sh has made the
// venv; elsewhere that one skips, and `make test` ends with test.sh --gate-note,
// which says so in the gate's own output (mw-gq6.644: the full run once failed
// and the gate, which had skipped it, said nothing).

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScorerTestsThatNeedNoModel(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	if out, err := exec.Command("scorer/test.sh", "--pure").CombinedOutput(); err != nil {
		t.Fatalf("contrib/scorer/test.sh --pure: %v\n%s", err, out)
	}
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
	if out, err := exec.Command("scorer/test.sh").CombinedOutput(); err != nil {
		t.Fatalf("contrib/scorer/test.sh: %v\n%s", err, out)
	}
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
	cmd := exec.Command("scorer/test.sh", args...)
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

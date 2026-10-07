package contrib_test

// The scorer's own tests are Python, in contrib/scorer/tests, run by
// contrib/scorer/test.sh. The gate never needs torch: it runs the tests that
// need no model whenever python3 is here, and every test, the three fixture
// clips included, only on a host where contrib/scorer/install.sh has made the
// venv; elsewhere that one skips.

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if out, err := exec.Command("scorer/test.sh").CombinedOutput(); err != nil {
		t.Fatalf("contrib/scorer/test.sh: %v\n%s", err, out)
	}
}

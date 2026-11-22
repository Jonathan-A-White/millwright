package steps

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A scenario's temporary directory must go when the scenario ends: the steps
// left 370,000 of them in the home's /tmp (mw-gq6.1635).
func TestTempDirGoesWhenTheScenarioEnds(t *testing.T) {
	dir, err := scenarioTempDir("", "mw-tempdir-test-")
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	removeScenarioTempDirs()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		os.Chmod(locked, 0o700)
		os.RemoveAll(dir)
		t.Fatalf("%s is still there after the scenario ended (stat error: %v)", dir, err)
	}
}

// bareMkdirTemp finds a call of os.MkdirTemp.
var bareMkdirTemp = regexp.MustCompile(`\bos\.MkdirTemp\(`)

// No step file makes a temporary directory by hand: only scenarioTempDir may call
// os.MkdirTemp, because only it registers the directory's removal.
func TestStepsMakeTheirTempDirsWithTheHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("listing the step files: %v (%d found)", err, len(files))
	}
	for _, file := range files {
		if file == "tempdir.go" || strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if bareMkdirTemp.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d makes a temporary directory with os.MkdirTemp that nothing removes; use scenarioTempDir", file, i+1)
			}
		}
	}
}

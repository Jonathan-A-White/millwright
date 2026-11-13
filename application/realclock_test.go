package application_test

// scripts/check-realclock.sh, which make lint runs, refuses a new time.Now()
// or time.Since( in application/ code that is not a test (mw-gq6.1140: a wait
// bound read off the host's real clock had its test margin fail under load
// three times, mw-gq6.983, mw-gq6.1030, mw-gq6.991). The uses already there are
// counted per file in scripts/realclock-baseline.txt; a use the baseline does
// not cover needs an injected clock, or an allow comment, //mw:realclock and
// why. These tests run the script on throwaway trees, and on this repository.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkRealclock(t *testing.T, root string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/check-realclock.sh")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", script, root)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// aRealclockTree writes files (repo-relative path to content) into a fresh
// directory and returns it.
func aRealclockTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const realclockBaselinePath = "scripts/realclock-baseline.txt"

const aWaitOnTheRealClock = `package application

import "time"

func waitFor(ready func() bool) bool {
	start := time.Now()
	for !ready() {
		if time.Since(start) > time.Minute {
			return false
		}
	}
	return true
}
`

func TestRealclockCheckPassesABaselinedUse(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/wait.go": aWaitOnTheRealClock,
		realclockBaselinePath: "# uses already there\napplication/wait.go 2\n",
	})
	if out, err := checkRealclock(t, root); err != nil {
		t.Fatalf("a baselined use was refused: %v\n%s", err, out)
	}
}

func TestRealclockCheckFailsANewUseNamingItsLine(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/wait.go":  aWaitOnTheRealClock,
		"application/fresh.go": "package application\n\nimport \"time\"\n\nfunc stamp() time.Time {\n\treturn time.Now()\n}\n",
		realclockBaselinePath:  "application/wait.go 2\n",
	})
	out, err := checkRealclock(t, root)
	if err == nil {
		t.Fatalf("a new time.Now() passed:\n%s", out)
	}
	if !strings.Contains(out, "application/fresh.go:6") {
		t.Fatalf("the refusal does not name application/fresh.go:6:\n%s", out)
	}
	if strings.Contains(out, "application/wait.go") {
		t.Fatalf("the refusal names the baselined application/wait.go:\n%s", out)
	}
	if !strings.Contains(out, "//mw:realclock") {
		t.Fatalf("the refusal does not say how to allow a deliberate use:\n%s", out)
	}
}

func TestRealclockCheckFailsAUseBeyondAFilesBaselinedCount(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/wait.go": aWaitOnTheRealClock,
		realclockBaselinePath: "application/wait.go 1\n",
	})
	out, err := checkRealclock(t, root)
	if err == nil {
		t.Fatalf("two uses in a file baselined at one passed:\n%s", out)
	}
	if !strings.Contains(out, "application/wait.go") {
		t.Fatalf("the refusal does not name application/wait.go:\n%s", out)
	}
}

func TestRealclockCheckFailsANewUseInASubdirectory(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/apptest/fake.go": "package apptest\n\nimport \"time\"\n\nvar since = time.Since(time.Time{})\n",
		realclockBaselinePath:         "",
	})
	out, err := checkRealclock(t, root)
	if err == nil || !strings.Contains(out, "application/apptest/fake.go:5") {
		t.Fatalf("a new time.Since( under application/apptest was not refused by its line (%v):\n%s", err, out)
	}
}

func TestRealclockCheckLetsAnAllowCommentWithAReasonExemptAUse(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/same.go":  "package application\n\nimport \"time\"\n\nvar a = time.Now() //mw:realclock a log stamp, not a bound\n",
		"application/above.go": "package application\n\nimport \"time\"\n\n//mw:realclock the default of the injected clock\n// when none is given.\nvar b = time.Now()\n",
		realclockBaselinePath:  "",
	})
	if out, err := checkRealclock(t, root); err != nil {
		t.Fatalf("uses with an allow comment were refused: %v\n%s", err, out)
	}
}

func TestRealclockCheckRefusesAnAllowCommentWithNoReason(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/bare.go": "package application\n\nimport \"time\"\n\nvar a = time.Now() //mw:realclock\n",
		realclockBaselinePath: "",
	})
	out, err := checkRealclock(t, root)
	if err == nil || !strings.Contains(out, "application/bare.go:5") {
		t.Fatalf("an allow comment with no reason exempted a use (%v):\n%s", err, out)
	}
}

func TestRealclockCheckIgnoresTestsCommentsAndOtherDirectories(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/wait_test.go": aWaitOnTheRealClock,
		"application/doc.go":       "package application\n\n// waitFor once read time.Now() for its bound.\n",
		"infrastructure/x/x.go":    aWaitOnTheRealClock,
		realclockBaselinePath:      "",
	})
	if out, err := checkRealclock(t, root); err != nil {
		t.Fatalf("a test, a comment or a file outside application/ was refused: %v\n%s", err, out)
	}
}

func TestRealclockCheckFailsWithNoBaselineFile(t *testing.T) {
	root := aRealclockTree(t, map[string]string{"application/wait.go": aWaitOnTheRealClock})
	out, err := checkRealclock(t, root)
	if err == nil || !strings.Contains(out, realclockBaselinePath) {
		t.Fatalf("a tree with no baseline file passed, or the refusal does not name it (%v):\n%s", err, out)
	}
}

func TestRealclockCheckPassesAndNotesAFileBelowItsBaseline(t *testing.T) {
	root := aRealclockTree(t, map[string]string{
		"application/wait.go": aWaitOnTheRealClock,
		realclockBaselinePath: "application/wait.go 3\napplication/gone.go 1\n",
	})
	out, err := checkRealclock(t, root)
	if err != nil {
		t.Fatalf("a file below its baseline was refused: %v\n%s", err, out)
	}
	if !strings.Contains(out, "application/wait.go") || !strings.Contains(out, "application/gone.go") {
		t.Fatalf("the pass does not note the files whose baseline can be lowered:\n%s", out)
	}
}

func TestRealclockCheckPassesThisRepository(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := checkRealclock(t, root); err != nil {
		t.Fatalf("application/ holds a time.Now() or time.Since( that scripts/realclock-baseline.txt does not cover: %v\n%s", err, out)
	}
}

package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// shapeCheckout is a git checkout of a rig whose history the SHAPE part reads:
// a file with a hundred lines made in May, outside both windows; cool.go made on
// 2026-08-01 and changed nine times in October; hot.go made in October and
// changed eleven more times, with twenty-five tests that call it; gone.go made
// and deleted in October; and a README that is no code. Today is 2026-10-20 and
// the report asks for 30 days, from 2026-09-20.
func shapeCheckout(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	git := func(date string, args ...string) {
		t.Helper()
		cmd := gittest.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	appendLine := func(name string) {
		t.Helper()
		f, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(name)), os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.WriteString("// more\n"); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(date, message string) {
		t.Helper()
		git(date, "add", "-A")
		git(date, "commit", "-q", "-m", message)
	}
	body := func(first string, lines int) string {
		text := first + "\n"
		for i := 1; i < lines; i++ {
			text += "// line\n"
		}
		return text
	}
	git("2026-05-01T12:00:00Z", "init", "-q")
	write("app/ancient.go", "package app\n\nfunc Ancient() {}\n"+strings.Repeat("// old\n", 100))
	commit("2026-05-01T12:00:00Z", "ancient")
	write("app/cool.go", body("package app", 4)+"func Cool() {}\n")
	write("README.md", "# the rig\n")
	commit("2026-08-01T12:00:00Z", "cool is made")
	for i := 0; i < 25; i++ {
		write(fmt.Sprintf("app/hot_%02d_test.go", i), "package app\n\nfunc TestHot() { Hot() }\n")
	}
	for i := 0; i < 3; i++ {
		write(fmt.Sprintf("app/cool_%02d_test.go", i), "package app\n\nfunc TestCool() { Cool() }\n")
	}
	commit("2026-10-01T12:00:00Z", "the tests")
	write("app/hot.go", body("package app", 9)+"func Hot() {}\n")
	write("app/gone.go", "package app\n\nfunc Gone() {}\n// a\n")
	commit("2026-10-02T12:00:00Z", "hot and gone are made")
	for i := 0; i < 11; i++ {
		appendLine("app/hot.go")
		if i < 9 {
			appendLine("app/cool.go")
		}
		commit(fmt.Sprintf("2026-10-%02dT12:00:00Z", 3+i), fmt.Sprintf("change %d", i))
	}
	if err := os.Remove(filepath.Join(dir, "app", "gone.go")); err != nil {
		t.Fatal(err)
	}
	commit("2026-10-15T12:00:00Z", "gone is deleted")
	return dir
}

// shapeReport runs mw report for the rig on the fixture checkout, 30 days.
func shapeReport(t *testing.T, dir string, rigName string, asJSON bool) (application.FactoryReportResult, string) {
	t.Helper()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	var out strings.Builder
	report := application.FactoryReport{
		Tracker: apptest.NewFakeTracker(), Files: vault.New(t.TempDir()), Seat: "builder", Benchmarks: &apptest.FakeBenchmarks{},
		Since: now.AddDate(0, 0, -30), Rig: rigName, JSON: asJSON,
		Shape: rig.Shape{}, RigDirs: map[string]string{"millwright": dir},
		Now: func() time.Time { return now }, Out: &out,
	}
	result, err := report.Run(context.Background())
	if err != nil {
		t.Fatalf("mw report failed: %v", err)
	}
	return result, out.String()
}

func TestFactoryReportShapePrintsTheHotFilesTheAllowanceAndWhatWasRemoved(t *testing.T) {
	dir := shapeCheckout(t)
	result, printed := shapeReport(t, dir, "millwright", false)

	shape := result.Shape
	if shape == nil {
		t.Fatalf("the report has no SHAPE:\n%s", printed)
	}
	// hot.go: 12 commits, 21 lines; cool.go: 10 commits, 14 lines. gone.go is
	// not there today, ancient.go was not changed in 90 days, tests are no code.
	if len(shape.Hot) != 2 {
		t.Fatalf("hot files %+v, want hot.go and cool.go", shape.Hot)
	}
	if first := shape.Hot[0]; first.Path != "app/hot.go" || first.Commits != 12 || first.Lines != 21 || first.TestCallers != 25 {
		t.Errorf("the hottest file is %+v, want app/hot.go with 12 commits, 21 lines and 25 test callers", first)
	}
	if second := shape.Hot[1]; second.Path != "app/cool.go" || second.Commits != 10 || second.Lines != 14 || second.TestCallers != 3 {
		t.Errorf("the next file is %+v, want app/cool.go with 10 commits, 14 lines and 3 test callers", second)
	}
	if shape.HotFiles != 2 || shape.OverAllowance != 1 || shape.Allowance != 20 {
		t.Errorf("over the allowance %d of %d (allowance %d), want 1 of 2 (20)", shape.OverAllowance, shape.HotFiles, shape.Allowance)
	}
	// In the 30 days: hot.go +21, cool.go +9, gone.go +4 and -4, one file deleted.
	if shape.Added != 34 || shape.Removed != 4 || shape.CodeFilesDeleted != 1 {
		t.Errorf("lines added %d, removed %d, code files deleted %d; want 34, 4 and 1", shape.Added, shape.Removed, shape.CodeFilesDeleted)
	}

	for _, want := range []string{
		"\nSHAPE millwright\n",
		"app/hot.go",
		"12 commits, 21 lines, 25 test callers",
		"10 commits, 14 lines, 3 test callers",
		"hot files over the allowance 1 of 2\n",
		"lines added 34, removed 4, code files deleted 1\n",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("the printed report lacks %q:\n%s", want, printed)
		}
	}
	for _, line := range strings.Split(printed, "\n") {
		if utf8.RuneCountInString(line) > application.ReportWidth {
			t.Errorf("a line is wider than %d columns: %q", application.ReportWidth, line)
		}
	}
}

func TestFactoryReportShapeIsInTheJSONUnderShape(t *testing.T) {
	dir := shapeCheckout(t)
	_, printed := shapeReport(t, dir, "millwright", true)

	var got struct {
		Shape struct {
			Rig string `json:"rig"`
			Hot []struct {
				Path        string `json:"path"`
				Commits     int    `json:"commits"`
				Lines       int    `json:"lines"`
				TestCallers int    `json:"test_callers"`
			} `json:"hot_files"`
			HotFiles         int `json:"hot_file_count"`
			OverAllowance    int `json:"over_allowance"`
			Added            int `json:"lines_added"`
			Removed          int `json:"lines_removed"`
			CodeFilesDeleted int `json:"code_files_deleted"`
		} `json:"shape"`
	}
	if err := json.Unmarshal([]byte(printed), &got); err != nil {
		t.Fatalf("the JSON does not read: %v\n%s", err, printed)
	}
	s := got.Shape
	if s.Rig != "millwright" || len(s.Hot) != 2 || s.Hot[0].Path != "app/hot.go" || s.Hot[0].TestCallers != 25 {
		t.Errorf("shape.hot_files is %+v for rig %q, want app/hot.go with 25 test callers first", s.Hot, s.Rig)
	}
	if s.HotFiles != 2 || s.OverAllowance != 1 || s.Added != 34 || s.Removed != 4 || s.CodeFilesDeleted != 1 {
		t.Errorf("shape is %+v, want 1 of 2 over, 34 added, 4 removed, 1 deleted", s)
	}
}

func TestFactoryReportShapeIsLeftOutOfAReportOfNoRig(t *testing.T) {
	dir := shapeCheckout(t)
	result, printed := shapeReport(t, dir, "", false)
	if result.Shape != nil || strings.Contains(printed, "SHAPE") {
		t.Errorf("a report of every rig has a SHAPE:\n%s", printed)
	}
}

func TestFactoryReportShapeSaysWhenTheRigHasNoCheckoutHere(t *testing.T) {
	dir := shapeCheckout(t)
	result, printed := shapeReport(t, dir, "lampas", false)
	if result.Shape == nil || result.Shape.Unread == "" {
		t.Fatalf("a rig with no checkout on this host has %+v, want a SHAPE that says it was not read", result.Shape)
	}
	if !strings.Contains(printed, "SHAPE lampas\n") || !strings.Contains(printed, "not read: ") {
		t.Errorf("the printed report does not say SHAPE was not read:\n%s", printed)
	}
}

func TestFactoryReportShapeSaysWhenTheCheckoutIsNotGit(t *testing.T) {
	notGit := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	result, printed := shapeReport(t, notGit, "millwright", false)
	if result.Shape == nil || !strings.Contains(result.Shape.Unread, "git") {
		t.Fatalf("a checkout that is not git has %+v, want a SHAPE that says why it was not read\n%s", result.Shape, printed)
	}
}

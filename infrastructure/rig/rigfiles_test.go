package rig_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestARigsFilesAreFoundAsFilesDirectoriesAndGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"src/todo/Board.tsx", "docs/a/b/c.md"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := rig.Files{}
	for pattern, want := range map[string]bool{
		"src/todo/Board.tsx": true,
		"src/todo/Gone.tsx":  false,
		"src/todo":           true,
		"src/todo/*.tsx":     true,
		"src/todo/*.ts":      false,
		"src/other/*.tsx":    false,
		"docs/**/*.md":       true,
		"nope/**/*.md":       false,
	} {
		if got := files.Exists(dir, pattern); got != want {
			t.Errorf("Exists(%q) = %v, want %v", pattern, got, want)
		}
	}
}

func TestADirectorysFilesAreCountedUpToOnePastTheMost(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"src/a.ts", "src/b.ts", "src/c/d.ts", "src/node_modules/x.js", "src/.cache/y", ".git/HEAD", "top.md"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := rig.Files{}
	for _, c := range []struct {
		dir   string
		most  int
		n     int
		isDir bool
	}{
		{"src", 20, 3, true},
		{"src", 1, 2, true},
		{"", 20, 4, true},
		{"src/a.ts", 20, 0, false},
		{"gone", 20, 0, false},
	} {
		if n, isDir := files.FilesIn(dir, c.dir, c.most); n != c.n || isDir != c.isDir {
			t.Errorf("FilesIn(%q, %d) = %d, %v; want %d, %v", c.dir, c.most, n, isDir, c.n, c.isDir)
		}
	}
}

func TestAGlobsFilesAreCountedUpToOnePastTheMost(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tests/e2e/a.ts", "tests/e2e/b.ts", "tests/e2e/c.json", "tests/e2e/sub/d.ts",
		"tests/e2e/node_modules/x.ts", "tests/unit/e.ts", "top.ts"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := rig.Files{}
	for _, c := range []struct {
		pattern string
		most    int
		n       int
	}{
		{"tests/e2e/*.ts", 20, 2},
		{"tests/e2e/*.ts", 1, 2},
		{"tests/**/*.ts", 20, 4},
		{"tests/*/*.ts", 20, 3},
		{"*.ts", 20, 1},
		{"gone/*.ts", 20, 0},
	} {
		if n := files.Matching(dir, c.pattern, c.most); n != c.n {
			t.Errorf("Matching(%q, %d) = %d; want %d", c.pattern, c.most, n, c.n)
		}
	}
}

// trackedRepo is a temp git repo holding the files, all added.
func trackedRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestTestCallersAreTheTrackedTestFilesThatNameWhatAFileDeclares(t *testing.T) {
	dir := trackedRepo(t, map[string]string{
		"src/screens/Settings.tsx": "export function Settings() {}\nexport const Panel = 1\nfunction hidden() {}\n",
		"src/reader.go":            "package src\n\nfunc Reader() {}\n\nfunc (r T) Method() {}\n\nfunc unexported() {}\n",
		"features/a.feature":       "open Settings\n",
		"tests/b.test.ts":          "import { x } from './screens/Settings'\n",
		"tests/c.test.ts":          "import Panel from 'nowhere'\n",
		"src/c_test.go":            "// Reader\n",
		"src/d_test.go":            "// Method and unexported\n",
		"tests/other.test.ts":      "settings in lower case, Settingsy and hidden\n",
		"docs/notes.md":            "Settings and Reader in prose\n",
	})
	files := rig.Files{}
	for _, c := range []struct {
		named []string
		want  int
	}{
		{[]string{"src/screens/Settings.tsx"}, 3},
		{[]string{"src/reader.go"}, 1},
		{[]string{"src/screens/Settings.tsx", "src/reader.go"}, 4},
		{[]string{"./src/reader.go", "src/reader.go"}, 1},
		{[]string{"src/gone.go"}, 0},
	} {
		got, err := files.TestCallers(dir, c.named)
		if err != nil || got != c.want {
			t.Errorf("TestCallers(%q) = %d, %v; want %d", c.named, got, err, c.want)
		}
	}
	if _, err := files.TestCallers(t.TempDir(), []string{"src/reader.go"}); err == nil {
		t.Error("a directory that is no checkout was searched without an error")
	}
}

func TestACommonWordInTestsIsNotACallerOfAFileNamedByIt(t *testing.T) {
	tests := map[string]string{"application/status.go": "package application\n\nfunc Uptime() {}\n"}
	for i := 0; i < 10; i++ {
		tests[fmt.Sprintf("application/say%d_test.go", i)] = "package application\n\n// the status line\nvar _ = \"status\"\n"
	}
	for i := 0; i < 2; i++ {
		tests[fmt.Sprintf("application/call%d_test.go", i)] = "package application\n\nvar _ = Uptime()\n"
	}
	dir := trackedRepo(t, tests)
	got, err := rig.Files{}.TestCallers(dir, []string{"application/status.go"})
	if err != nil || got != 2 {
		t.Errorf("TestCallers = %d, %v; want 2 (the twelve test files that say status are not callers)", got, err)
	}
}

func TestTwentyStepFilesCallingAHelperAreTwentyCallers(t *testing.T) {
	steps := map[string]string{"features/steps/helper.go": "package steps\n\nfunc SharedHelper() {}\n"}
	for i := 0; i < 20; i++ {
		steps[fmt.Sprintf("features/steps/step%d_test.go", i)] = "package steps\n\nvar _ = SharedHelper\n"
	}
	dir := trackedRepo(t, steps)
	got, err := rig.Files{}.TestCallers(dir, []string{"features/steps/helper.go"})
	if err != nil || got != 20 {
		t.Errorf("TestCallers = %d, %v; want 20", got, err)
	}
}

func TestAFieldWithTheNameOfATypeIsNotACallOfTheType(t *testing.T) {
	dir := trackedRepo(t, map[string]string{
		"application/status.go":        "package application\n\ntype Status struct{}\n",
		"application/field_test.go":    "package application\n\nvar _ = story.Status\nvar _ = map[string]int{Status: 1}\nvar _ = row{Status: \"open\"}\n",
		"application/literal_test.go":  "package application\n\nvar _ = Status{}\n",
		"application/declared_test.go": "package application\n\nvar s Status\n",
		"cmd/mw/qualified_test.go":     "package main\n\nvar _ = application.Status{}\n",
		"cmd/mw/field_test.go":         "package main\n\nvar _ = story.Status\n",
	})
	got, err := rig.Files{}.TestCallers(dir, []string{"application/status.go"})
	if err != nil || got != 3 {
		t.Errorf("TestCallers = %d, %v; want 3 (the literal, the variable and the qualified use)", got, err)
	}
}

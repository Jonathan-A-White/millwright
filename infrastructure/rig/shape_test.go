package rig_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/gittest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func gitCommand(dir string, args ...string) *exec.Cmd {
	return gittest.Command("git", append([]string{"-C", dir}, args...)...)
}

func TestShapeCountsEachFilesTestCallersAsFilesDoesForThatFileAlone(t *testing.T) {
	dir := trackedRepo(t, map[string]string{
		"src/screens/Settings.tsx": "export function Settings() {}\nexport const Panel = 1\n",
		"src/reader.go":            "package src\n\nfunc Reader() {}\n",
		"features/a.feature":       "open Settings\n",
		"tests/b.test.ts":          "import { x } from './screens/Settings'\n",
		"src/c_test.go":            "// Reader\n",
		"src/d_test.go":            "package src\n\nfunc TestD() { Reader() }\n",
	})
	named := []string{"src/screens/Settings.tsx", "src/reader.go", "src/gone.go"}
	got, err := rig.Shape{}.TestCallersOf(dir, named)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range named {
		want, err := rig.Files{}.TestCallers(dir, []string{file})
		if err != nil || got[file] != want {
			t.Errorf("TestCallersOf[%s] = %d; TestCallers = %d, %v", file, got[file], want, err)
		}
	}
	if got["src/reader.go"] != 2 || got["src/screens/Settings.tsx"] != 2 {
		t.Errorf("counts %v, want reader.go 2 and Settings.tsx 2", got)
	}
	if _, err := (rig.Shape{}).TestCallersOf(t.TempDir(), named); err == nil {
		t.Error("a directory that is no checkout was searched without an error")
	}
}

func TestShapeReadsARenameUnderItsNewNameADeletionAndNoMerges(t *testing.T) {
	dir := trackedRepo(t, map[string]string{"a.go": "package a\n\nfunc A() {}\n// one\n// two\n// three\n", "b.go": "package a\n// b\n"})
	git := func(args ...string) {
		t.Helper()
		if out, err := gitCommand(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("-c", "user.name=T", "-c", "user.email=t@example.test", "commit", "-q", "-m", "first")
	git("mv", "a.go", "renamed.go")
	git("rm", "-q", "b.go")
	git("-c", "user.name=T", "-c", "user.email=t@example.test", "commit", "-q", "-m", "second")

	since := time.Now().Add(-time.Hour)
	changes, err := rig.Shape{}.Changes(context.Background(), dir, since)
	if err != nil {
		t.Fatal(err)
	}
	var renamed, removedB bool
	for _, c := range changes {
		renamed = renamed || c.Path == "renamed.go" && c.Added == 0 && c.Removed == 0
		removedB = removedB || c.Path == "b.go" && c.Added == 0 && c.Removed == 2
	}
	if !renamed || !removedB || len(changes) != 4 {
		t.Errorf("changes %+v, want a.go and b.go added, then renamed.go with no lines changed and b.go with 2 removed", changes)
	}
	deleted, err := rig.Shape{}.Deleted(context.Background(), dir, since)
	if err != nil || len(deleted) != 1 || deleted[0].Path != "b.go" {
		t.Errorf("deleted %+v, %v; want b.go alone", deleted, err)
	}
	if n, err := (rig.Shape{}).Lines(dir, "renamed.go"); err != nil || n != 5 {
		t.Errorf("Lines(renamed.go) = %d, %v; want 5 non-blank lines", n, err)
	}
}

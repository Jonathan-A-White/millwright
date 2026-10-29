package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func tocLongDoc(sections int) string {
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for i := 1; i <= sections; i++ {
		b.WriteString("## Section " + strings.Repeat("x", i) + "\n\ntext\n\n")
	}
	return b.String()
}

// tocRepo is a git repository holding one tracked and one untracked long
// document, a tracked changelog and a tracked short one.
func tocRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for file, text := range map[string]string{
		"tracked.md":        tocLongDoc(5),
		"untracked.md":      tocLongDoc(5),
		"CHANGELOG.md":      tocLongDoc(10),
		"short.md":          tocLongDoc(2),
		"node_modules/a.md": tocLongDoc(5),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "tracked.md", "CHANGELOG.md", "short.md", "node_modules/a.md"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestTocWithNoPathsChecksTheTrackedMarkdownOnly(t *testing.T) {
	dir := tocRepo(t)
	var out bytes.Buffer
	report, err := application.Toc{Files: diskTocFiles{dir: dir}, Out: &out}.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "tracked.md: TOC missing (5 ## headings)\nmw toc fix tracked.md\n"
	if out.String() != want || !report.Failed() {
		t.Fatalf("check printed %q, want %q", out.String(), want)
	}
}

func TestTocFixOnDiskIsIdempotent(t *testing.T) {
	dir := tocRepo(t)
	toc := application.Toc{Files: diskTocFiles{dir: dir}}
	if _, err := toc.Fix(context.Background()); err != nil {
		t.Fatal(err)
	}
	once, err := os.ReadFile(filepath.Join(dir, "tracked.md"))
	if err != nil || !strings.Contains(string(once), "<!-- toc -->") {
		t.Fatalf("tracked.md has no table after fix (%v):\n%s", err, once)
	}
	if untracked, _ := os.ReadFile(filepath.Join(dir, "untracked.md")); string(untracked) != tocLongDoc(5) {
		t.Fatalf("the untracked doc was touched:\n%s", untracked)
	}
	if _, err := toc.Fix(context.Background()); err != nil {
		t.Fatal(err)
	}
	twice, _ := os.ReadFile(filepath.Join(dir, "tracked.md"))
	if !bytes.Equal(once, twice) {
		t.Fatalf("a second fix changed the file:\n%s", twice)
	}
	var out bytes.Buffer
	if report, err := (application.Toc{Files: diskTocFiles{dir: dir}, Out: &out}).Check(context.Background()); err != nil || report.Failed() || out.Len() != 0 {
		t.Fatalf("check after fix: failed %v, err %v, printed %q", report.Failed(), err, out.String())
	}
}

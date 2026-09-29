package contrib_test

// docs/codemap.md is merged by git's union driver (.gitattributes), so two
// branches that each add a row to the same table both keep their row instead of
// conflicting. These tests prove that in a throwaway repository in a temporary
// directory, and that scripts/check-codemap.sh refuses the one thing a union
// merge can leave behind: a row that two branches edited differently.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestCodemapAttributeIsUnion(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	out := gitIn(t, root, "check-attr", "merge", "docs/codemap.md")
	if want := "docs/codemap.md: merge: union"; strings.TrimSpace(out) != want {
		t.Fatalf("git check-attr merge docs/codemap.md = %q, want %q", strings.TrimSpace(out), want)
	}
}

func TestCodemapUnionMergesTwoAddedRows(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf("no .gitattributes: %v", err)
	}
	codemap, err := os.ReadFile(filepath.Join(root, "docs", "codemap.md"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(dir, "docs", "codemap.md")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), attributes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapPath, codemap, 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")

	// Each branch adds a different row right after the same existing row of
	// the Ports table: adjacent lines, which a plain merge refuses.
	const anchor = "| `WorkTracker` |"
	addRow := func(branch, row string) {
		gitIn(t, dir, "checkout", "-q", "-b", branch, "main")
		lines := strings.Split(string(codemap), "\n")
		var edited []string
		found := false
		for _, l := range lines {
			edited = append(edited, l)
			if strings.HasPrefix(l, anchor) {
				edited = append(edited, row)
				found = true
			}
		}
		if !found {
			t.Fatalf("docs/codemap.md has no row starting %q to add beside", anchor)
		}
		if err := os.WriteFile(mapPath, []byte(strings.Join(edited, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "commit", "-q", "-am", branch)
	}
	rowA := "| `PortAlpha` | `application/alpha.go` | none | none |"
	rowB := "| `PortBeta` | `application/beta.go` | none | none |"
	addRow("alpha", rowA)
	addRow("beta", rowB)

	gitIn(t, dir, "checkout", "-q", "main")
	gitIn(t, dir, "merge", "--no-edit", "alpha")
	gitIn(t, dir, "merge", "--no-edit", "beta")

	merged, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{rowA, rowB} {
		if n := strings.Count(string(merged), row+"\n"); n != 1 {
			t.Errorf("merged codemap holds %q %d times, want once", row, n)
		}
	}
}

// checkCodemapIn runs a copy of scripts/check-codemap.sh over a codemap of the
// given text in a throwaway repository, and returns its exit status and output.
func checkCodemapIn(t *testing.T, codemap string) (int, string) {
	t.Helper()
	script, err := filepath.Abs("../scripts/check-codemap.sh")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for path, content := range map[string]string{
		"scripts/check-codemap.sh": string(body),
		"docs/codemap.md":          codemap,
		"docs/adding-a-command.md": "# Adding a command\n",
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"application", "cmd/mw"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "init", "-q")
	cmd := exec.Command("sh", "scripts/check-codemap.sh")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), string(out)
	}
	t.Fatal(err)
	return -1, ""
}

func TestCheckCodemapRefusesDuplicateRows(t *testing.T) {
	table := "| Port | Declared in |\n| --- | --- |\n"
	cases := []struct {
		name, codemap, wantLine string
	}{
		{
			name:     "same first cell, different rest",
			codemap:  "# Map\n\n" + table + "| `Runner` | `a` |\n| `Other` | `b` |\n| `Runner` | `c` |\n",
			wantLine: "line 7",
		},
		{
			name:     "identical line",
			codemap:  "# Map\n\nSee the table.\n\nSee the table.\n",
			wantLine: "line 5",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out := checkCodemapIn(t, c.codemap)
			if code == 0 {
				t.Fatalf("check-codemap passed on a duplicate, output:\n%s", out)
			}
			if !strings.Contains(out, c.wantLine) {
				t.Errorf("output does not name %q:\n%s", c.wantLine, out)
			}
		})
	}
}

func TestCheckCodemapAcceptsDistinctRowsUnderRepeatedSeparators(t *testing.T) {
	// Every table has the same separator line; that is not a duplicate.
	codemap := "# Map\n\n| Port | Declared in |\n| --- | --- |\n| `A` | `x` |\n\n" +
		"| Use case | File |\n| --- | --- |\n| `B` | `x` |\n"
	if code, out := checkCodemapIn(t, codemap); code != 0 {
		t.Fatalf("check-codemap refused distinct rows (exit %d):\n%s", code, out)
	}
}

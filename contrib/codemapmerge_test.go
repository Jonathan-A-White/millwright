package contrib_test

// docs/codemap.md is NOT merged by git's union driver: union keeps both
// versions of a row two branches each edited, and the two trimmed versions of a
// byte-capped page came out as one over the cap (mw-gq6.317). With the default
// merge, a row edited on two branches is a conflict a Builder resolves. These
// tests prove that in a throwaway repository in a temporary directory, that the
// real map passes scripts/check-codemap.sh (make test is what a landing runs),
// and that the script refuses the one thing a union merge could leave behind: a
// row that two branches edited differently.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestCodemapIsNotMergedByUnion(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(gitIn(t, root, "check-attr", "merge", "docs/codemap.md"))
	if strings.HasSuffix(out, ": union") {
		t.Fatalf("git check-attr merge docs/codemap.md = %q: a union merge doubles a row two branches edited", out)
	}
}

func TestCodemapRowEditedOnTwoBranchesNeverMergesToTwoRows(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	// The rig's own attributes, if it has any: there is no file when nothing
	// in the rig is merged specially.
	attributes, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
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
	if attributes != nil {
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), attributes, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(mapPath, codemap, 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "base")

	// Each branch trims the same row, differently.
	const anchor = "| `WorkTracker` |"
	editRow := func(branch, suffix string) {
		gitIn(t, dir, "checkout", "-q", "-b", branch, "main")
		lines := strings.Split(string(codemap), "\n")
		found := false
		for i, l := range lines {
			if strings.HasPrefix(l, anchor) {
				lines[i] = l + suffix
				found = true
			}
		}
		if !found {
			t.Fatalf("docs/codemap.md has no row starting %q to edit", anchor)
		}
		if err := os.WriteFile(mapPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "commit", "-q", "-am", branch)
	}
	editRow("alpha", " alpha")
	editRow("beta", " beta")

	gitIn(t, dir, "checkout", "-q", "main")
	gitIn(t, dir, "merge", "--no-edit", "alpha")
	if _, err := gitTry(dir, "merge", "--no-edit", "beta"); err != nil {
		return // a conflict: the Builder resolves it by hand
	}
	merged, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(merged), anchor); n != 1 {
		t.Errorf("merged codemap holds a row starting %q %d times, want once", anchor, n)
	}
}

func TestRealCodemapPassesItsCheck(t *testing.T) {
	// make test is what a landing runs; make lint is not. Without this the
	// byte cap and the duplicate-row rule are only as good as a Builder's habit.
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "scripts/check-codemap.sh")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scripts/check-codemap.sh on the real map: %v\n%s", err, out)
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

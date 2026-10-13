package contrib_test

// docs/codemap.md is merged by git's union driver (.gitattributes), so two
// stories that each add a row near each other both keep it instead of
// conflicting (mw-gq6.408). Union also keeps both versions of a row two
// branches edited differently, and it can leave the page over its byte cap
// (mw-gq6.317); scripts/check-codemap.sh, which make test runs, is the net for
// those. These tests prove all of that in a throwaway repository in a
// temporary directory.

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

func TestCodemapIsMergedByUnion(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(gitIn(t, root, "check-attr", "merge", "docs/codemap.md"))
	if want := "docs/codemap.md: merge: union"; out != want {
		t.Fatalf("git check-attr merge docs/codemap.md = %q, want %q", out, want)
	}
}

// mergeCodemapBranches commits base (the real map when empty) and the rig's attributes in a
// throwaway repository, edits it on branches alpha and beta with the given
// functions, merges both into main and returns the merged map, plus whether
// the second merge succeeded.
func mergeCodemapBranches(t *testing.T, base string, alpha, beta func(lines []string) []string) (string, bool) {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	codemap := []byte(base)
	if base == "" {
		if codemap, err = os.ReadFile(filepath.Join(root, "docs", "codemap.md")); err != nil {
			t.Fatal(err)
		}
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

	edit := func(branch string, change func([]string) []string) {
		gitIn(t, dir, "checkout", "-q", "-b", branch, "main")
		lines := change(strings.Split(string(codemap), "\n"))
		if err := os.WriteFile(mapPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "commit", "-q", "-am", branch)
	}
	edit("alpha", alpha)
	edit("beta", beta)

	gitIn(t, dir, "checkout", "-q", "main")
	gitIn(t, dir, "merge", "--no-edit", "alpha")
	_, mergeErr := gitTry(dir, "merge", "--no-edit", "beta")
	merged, err := os.ReadFile(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(merged), mergeErr == nil
}

// insertAfterRow returns a change that puts row after the line starting anchor.
func insertAfterRow(t *testing.T, anchor, row string) func([]string) []string {
	return func(lines []string) []string {
		for i, l := range lines {
			if strings.HasPrefix(l, anchor) {
				out := append([]string{}, lines[:i+1]...)
				out = append(out, row)
				return append(out, lines[i+1:]...)
			}
		}
		t.Fatalf("docs/codemap.md has no row starting %q", anchor)
		return nil
	}
}

func TestTwoRowsAddedAtTheSameSpotMergeWithBothRows(t *testing.T) {
	const anchor = "| `WorkTracker` |"
	rowA := "| `AlphaPort` | application/alpha.go | infra/alpha | FakeAlpha |"
	rowB := "| `BetaPort` | application/beta.go | infra/beta | FakeBeta |"
	merged, ok := mergeCodemapBranches(t, "",
		insertAfterRow(t, anchor, rowA), insertAfterRow(t, anchor, rowB))
	if !ok {
		t.Fatalf("merging two added rows at the same spot conflicted:\n%s", merged)
	}
	for _, row := range []string{rowA, rowB} {
		if n := strings.Count(merged, row+"\n"); n != 1 {
			t.Errorf("merged codemap holds %q %d times, want once", row, n)
		}
	}
	if strings.Contains(merged, "<<<<<<<") {
		t.Errorf("merged codemap holds a conflict marker:\n%s", merged)
	}
}

func TestCheckCodemapRefusesARowTwoBranchesEdited(t *testing.T) {
	// Union keeps both versions of a row each branch edited; the check
	// scripts/check-codemap.sh makes in the gate is what catches them.
	const anchor = "| `WorkTracker` |"
	base := "# Map\n\n| Port | Declared in |\n| --- | --- |\n| `Other` | `a` |\n" +
		"| `WorkTracker` | `b` |\n| `Last` | `c` |\n"
	suffix := func(s string) func([]string) []string {
		return func(lines []string) []string {
			for i, l := range lines {
				if strings.HasPrefix(l, anchor) {
					lines[i] = l + s
					return lines
				}
			}
			t.Fatalf("docs/codemap.md has no row starting %q", anchor)
			return nil
		}
	}
	merged, ok := mergeCodemapBranches(t, base, suffix(" alpha"), suffix(" beta"))
	if !ok {
		return // a conflict: the Builder resolves it by hand
	}
	if n := strings.Count(merged, anchor); n != 2 {
		t.Fatalf("merged codemap holds a row starting %q %d times, want 2 (union)", anchor, n)
	}
	code, out := checkCodemapIn(t, merged)
	if code == 0 {
		t.Fatalf("check-codemap passed a map holding two versions of one row:\n%s", out)
	}
	if !strings.Contains(out, "same first cell") {
		t.Errorf("output does not name the repeated first cell:\n%s", out)
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

func TestCheckCodemapCapIs10240Bytes(t *testing.T) {
	// One filler line brings the map to an exact size; it repeats nothing.
	sized := func(n int) string {
		head := "# Map\n"
		return head + strings.Repeat("a", n-len(head)-1) + "\n"
	}
	if code, out := checkCodemapIn(t, sized(10240)); code != 0 {
		t.Fatalf("check-codemap refused a 10240-byte map (exit %d):\n%s", code, out)
	}
	code, out := checkCodemapIn(t, sized(10241))
	if code == 0 {
		t.Fatalf("check-codemap passed a 10241-byte map, output:\n%s", out)
	}
	for _, want := range []string{"10240", "merging rows"} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

func TestCheckCodemapRefusesAConflictMarker(t *testing.T) {
	table := "| Port | Declared in |\n| --- | --- |\n"
	for _, marker := range []string{"<<<<<<< HEAD", "<<<<<<<", "=======", ">>>>>>> mw/mw-x.1", ">>>>>>>"} {
		t.Run(marker, func(t *testing.T) {
			codemap := "# Map\n\n" + table + "| `A` | `x` |\n" + marker + "\n| `B` | `y` |\n"
			code, out := checkCodemapIn(t, codemap)
			if code == 0 {
				t.Fatalf("check-codemap passed a map holding %q, output:\n%s", marker, out)
			}
			if !strings.Contains(out, "conflict marker") || !strings.Contains(out, "line 6") {
				t.Errorf("output does not name a conflict marker on line 6:\n%s", out)
			}
		})
	}
}

func TestCheckCodemapAcceptsEqualsRunsThatAreNotMarkers(t *testing.T) {
	// A longer run of = (a setext heading underline) is not a marker.
	codemap := "# Map\n\nTitle\n==========\n\n| Port | Declared in |\n| --- | --- |\n| `A` | `x` |\n"
	if code, out := checkCodemapIn(t, codemap); code != 0 {
		t.Fatalf("check-codemap refused a long = line (exit %d):\n%s", code, out)
	}
}

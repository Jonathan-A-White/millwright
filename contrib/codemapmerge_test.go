package contrib_test

// docs/codemap.md is merged by scripts/codemap-merge.sh, named merge=codemap in
// .gitattributes (mw-gq6.495). Two stories that each add a row near each other
// both keep it, as union gave (mw-gq6.408); unlike union, a story that trims
// rows does not get the old rows back when main added one meanwhile
// (mw-gq6.480 merged at 13,844 bytes twice). A row two branches edited takes
// ours. scripts/check-codemap.sh, which make test runs, is still the net for
// the byte cap and repeated rows. These tests prove all of that in a throwaway
// repository in a temporary directory.

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

func TestCodemapIsMergedByTheCodemapDriver(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(gitIn(t, root, "check-attr", "merge", "docs/codemap.md"))
	if want := "docs/codemap.md: merge: codemap"; out != want {
		t.Fatalf("git check-attr merge docs/codemap.md = %q, want %q", out, want)
	}
}

// codemapDriver is the merge.codemap.driver mw passes on its merges: the
// rig's own scripts/codemap-merge.sh, with nothing in any git config.
func codemapDriver(t *testing.T) string {
	t.Helper()
	script, err := filepath.Abs("../scripts/codemap-merge.sh")
	if err != nil {
		t.Fatal(err)
	}
	return "merge.codemap.driver=sh '" + script + "' %O %A %B"
}

// mergeCodemapBranches commits base (the real map when empty) and the rig's attributes in a
// throwaway repository, edits it on branches alpha and beta with the given
// functions, merges both into main with the codemap driver passed as mw passes
// it and returns the merged map, plus whether the second merge succeeded.
func mergeCodemapBranches(t *testing.T, base string, alpha, beta func(lines []string) []string) (string, bool) {
	t.Helper()
	return mergeCodemapBranchesWith(t, codemapDriver(t), base, alpha, beta)
}

// mergeCodemapBranchesWith is mergeCodemapBranches with driver as the -c given
// to each merge, or none when it is empty: a merge by hand on a host whose git
// config does not name the driver.
func mergeCodemapBranchesWith(t *testing.T, driver, base string, alpha, beta func(lines []string) []string) (string, bool) {
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
	merge := func(branch string) []string {
		if driver == "" {
			return []string{"merge", "--no-edit", branch}
		}
		return []string{"-c", driver, "merge", "--no-edit", branch}
	}
	gitIn(t, dir, merge("alpha")...)
	_, mergeErr := gitTry(dir, merge("beta")...)
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

// editRow returns a change that appends s to the row starting anchor.
func editRow(t *testing.T, anchor, s string) func([]string) []string {
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

func TestARowTwoBranchesEditedTakesOurs(t *testing.T) {
	// main has alpha's edit when beta merges into it, so alpha's is ours.
	const anchor = "| `WorkTracker` |"
	base := "# Map\n\n| Port | Declared in |\n| --- | --- |\n| `Other` | `a` |\n" +
		"| `WorkTracker` | `b` |\n| `Last` | `c` |\n"
	merged, ok := mergeCodemapBranches(t, base, editRow(t, anchor, " alpha"), editRow(t, anchor, " beta"))
	if !ok {
		t.Fatalf("merging a row two branches edited conflicted:\n%s", merged)
	}
	if n := strings.Count(merged, anchor); n != 1 {
		t.Fatalf("merged codemap holds a row starting %q %d times, want once:\n%s", anchor, n, merged)
	}
	if !strings.Contains(merged, "| `WorkTracker` | `b` | alpha\n") {
		t.Errorf("merged codemap does not keep ours, alpha's edit:\n%s", merged)
	}
	if code, out := checkCodemapIn(t, merged); code != 0 {
		t.Errorf("check-codemap refused the merged map (exit %d):\n%s", code, out)
	}
}

func TestWithoutTheDriverAMergeStopsOnAConflictAndTheCheckRefusesIt(t *testing.T) {
	// A host whose git config does not name merge.codemap.driver (a merge
	// made by hand, not by mw) gets git's text merge: a conflict, said out
	// loud, never union, and the check that make test makes refuses the
	// markers if they are committed.
	const anchor = "| `WorkTracker` |"
	base := "# Map\n\n| Port | Declared in |\n| --- | --- |\n| `Other` | `a` |\n" +
		"| `WorkTracker` | `b` |\n| `Last` | `c` |\n"
	merged, ok := mergeCodemapBranchesWith(t, "", base, editRow(t, anchor, " alpha"), editRow(t, anchor, " beta"))
	if ok {
		t.Fatalf("a merge with no driver configured went through:\n%s", merged)
	}
	if !strings.Contains(merged, "<<<<<<<") {
		t.Fatalf("a merge with no driver configured left no conflict markers:\n%s", merged)
	}
	if code, out := checkCodemapIn(t, merged); code == 0 || !strings.Contains(out, "conflict marker") {
		t.Errorf("check-codemap did not refuse the conflicted map (exit %d):\n%s", code, out)
	}
}

// runDriver runs scripts/codemap-merge.sh on the three versions as git would,
// and returns what it left in ours.
func runDriver(t *testing.T, base, ours, theirs string) string {
	t.Helper()
	script, err := filepath.Abs("../scripts/codemap-merge.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	paths := map[string]string{"base": base, "ours": ours, "theirs": theirs}
	for name, text := range paths {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", script, "base", "ours", "theirs")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("codemap-merge.sh: %v\n%s", err, out)
	}
	merged, err := os.ReadFile(filepath.Join(dir, "ours"))
	if err != nil {
		t.Fatal(err)
	}
	return string(merged)
}

// realCodemap is docs/codemap.md as the rig has it.
func realCodemap(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../docs/codemap.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// trimmed is the map with every port row's last cell cut to its first eight
// bytes and the two rows starting drop taken out, as a trim of the map does.
func trimmed(t *testing.T, codemap string, drop ...string) string {
	t.Helper()
	var out []string
	ports := false
	dropped := 0
next:
	for _, line := range strings.Split(codemap, "\n") {
		for _, d := range drop {
			if strings.HasPrefix(line, d) {
				dropped++
				continue next
			}
		}
		if strings.HasPrefix(line, "## ") {
			ports = line == "## Ports"
		}
		if ports && strings.HasPrefix(line, "| ") && !strings.HasPrefix(line, "| Port ") {
			cells := strings.Split(strings.TrimSuffix(line, " |"), " | ")
			if last := cells[len(cells)-1]; len(last) > 8 {
				cells[len(cells)-1] = last[:8]
			}
			line = strings.Join(cells, " | ") + " |"
		}
		out = append(out, line)
	}
	if dropped != len(drop) {
		t.Fatalf("docs/codemap.md has %d of the rows %q", dropped, drop)
	}
	return strings.Join(out, "\n")
}

func TestTheDriverKeepsATrimAndTheRowTheOtherSideAdded(t *testing.T) {
	// Ours trims the real map, theirs adds one port row among the rows ours
	// shortened: the result is ours with theirs' row, not the union.
	base := realCodemap(t)
	ours := trimmed(t, base, "| `WorkTracker` |", "| WatchProbes |")
	row := "| `NewPort` | application/newport.go | infra/newport | FakeNewPort |"
	theirs := strings.Join(insertAfterRow(t, "| Reap{Terminal,Log,Armer} |", row)(strings.Split(base, "\n")), "\n")

	merged := runDriver(t, base, ours, theirs)

	shortReap := ""
	for _, line := range strings.Split(ours, "\n") {
		if strings.HasPrefix(line, "| Reap{Terminal,Log,Armer} |") {
			shortReap = line
		}
	}
	want := strings.Replace(ours, shortReap+"\n", shortReap+"\n"+row+"\n", 1)
	if merged != want {
		t.Errorf("merged map is not ours with theirs' row after the Reap row:\n%s", merged)
	}
	for _, line := range strings.Split(base, "\n") {
		if !strings.Contains(ours, line) && strings.Contains(merged, line+"\n") {
			t.Errorf("merged map brings back a row the trim shortened or took out: %q", line)
		}
	}
	if len(merged) > 10240 {
		t.Errorf("merged map is %d bytes, over the 10240 cap", len(merged))
	}
	if code, out := checkCodemapIn(t, merged); code != 0 {
		t.Errorf("check-codemap refused the merged map (exit %d):\n%s", code, out)
	}

	// The other way round, as a landing merges a trim into a main that added
	// the row: the same rows.
	other := runDriver(t, base, theirs, ours)
	if other != want {
		t.Errorf("merging the trim into theirs is not the trim with theirs' row:\n%s", other)
	}
}

func TestTheDriverKeepsTwoRowsAddedToOneSection(t *testing.T) {
	base := realCodemap(t)
	const anchor = "| WatchProbes |"
	rowA := "| `AlphaPort` | application/alpha.go | infra/alpha | FakeAlpha |"
	rowB := "| `BetaPort` | application/beta.go | infra/beta | FakeBeta |"
	lines := func(row string) string {
		return strings.Join(insertAfterRow(t, anchor, row)(strings.Split(base, "\n")), "\n")
	}
	merged := runDriver(t, base, lines(rowA), lines(rowB))
	for _, row := range []string{rowA, rowB} {
		if n := strings.Count(merged, row+"\n"); n != 1 {
			t.Errorf("merged map holds %q %d times, want once", row, n)
		}
	}
	if !strings.Contains(merged, anchor) || strings.Count(merged, "\n") != strings.Count(base, "\n")+2 {
		t.Errorf("merged map is not the base with the two rows:\n%s", merged)
	}
}

func TestTheDriverDropsARowTheOtherSideTookOut(t *testing.T) {
	base := "# Map\n\n## Ports\n\n| Port | Declared in |\n|-|-|\n| `A` | a |\n| `B` | b |\n| `C` | c |\n"
	ours := strings.Replace(base, "| `B` | b |\n", "| `B` | b |\n| `N` | n |\n", 1)
	theirs := strings.Replace(base, "| `B` | b |\n", "", 1)
	want := strings.Replace(base, "| `B` | b |\n", "| `N` | n |\n", 1)
	if merged := runDriver(t, base, ours, theirs); merged != want {
		t.Errorf("merged map keeps a row theirs took out, or loses ours:\n%s", merged)
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

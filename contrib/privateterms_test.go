package contrib_test

// scripts/check-private-terms.sh refuses a new tracked line that holds a term
// of a private list kept outside this repository (mw-t5shinh.19). These tests
// run it on throwaway repositories, an invented list and an invented baseline:
// no real term is in this file, and no test output may hold one.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// the invented terms; a leak of either into an output is a failure.
const (
	termOne = "zorblax"
	termTwo = "quenby wick"
)

// aTermsDir writes an invented list (a comment, a blank line, then terms) and
// returns the list's path. The baseline belongs beside it.
func aTermsDir(t *testing.T, terms ...string) string {
	t.Helper()
	dir := t.TempDir()
	list := filepath.Join(dir, "private-terms.txt")
	body := "# invented terms for a test\n\n" + strings.Join(terms, "\n") + "\n"
	if err := os.WriteFile(list, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return list
}

func privateTerms(t *testing.T, repo, list string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("../scripts/check-private-terms.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := command(t, "sh", append([]string{script}, append(args, repo)...)...)
	cmd.Env = append(os.Environ(), "MW_PRIVATE_TERMS="+list)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// baselineOf stores what --baseline prints for repo, beside list.
func baselineOf(t *testing.T, repo, list string) string {
	t.Helper()
	out, err := privateTerms(t, repo, list, "--baseline")
	if err != nil {
		t.Fatalf("--baseline: %v\n%s", err, out)
	}
	path := strings.TrimSuffix(list, ".txt") + ".baseline"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

func noLeak(t *testing.T, out string, parts ...string) {
	t.Helper()
	low := strings.ToLower(out)
	for _, p := range parts {
		if strings.Contains(low, strings.ToLower(p)) {
			t.Fatalf("the output holds %q:\n%s", p, out)
		}
	}
}

func commitAll(t *testing.T, repo string) {
	t.Helper()
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-qm", "more")
}

func TestPrivateTermsFailsAFileOverItsBaselineNamingFileLineAndListLine(t *testing.T) {
	list := aTermsDir(t, "other thing", termOne)
	repo := aRepoTracking(t, map[string][]byte{"docs/a.md": []byte("hello\nSee Zorblax here\nbye\n")})
	baselineOf(t, repo, list)
	if out, err := privateTerms(t, repo, list); err != nil {
		t.Fatalf("expected a pass at the baseline, got %v:\n%s", err, out)
	}

	// one more matching line (line 4) than the baseline allows.
	if err := os.WriteFile(filepath.Join(repo, "docs/a.md"), []byte("hello\nSee Zorblax here\nbye\nthe secret zorblax text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo)
	out, err := privateTerms(t, repo, list)
	if err == nil {
		t.Fatalf("expected a failure, got a pass:\n%s", out)
	}
	if !strings.Contains(out, "docs/a.md") {
		t.Fatalf("expected the file named:\n%s", out)
	}
	// the file's line 4; the list's line 4 (a comment, a blank, "other thing", the term).
	if !strings.Contains(out, "docs/a.md:4:") {
		t.Fatalf("expected docs/a.md:4 named:\n%s", out)
	}
	if !strings.Contains(out, "list line 4") {
		t.Fatalf("expected the term's list line 4 named:\n%s", out)
	}
	noLeak(t, out, termOne, "secret", "See ", "other thing")
}

func TestPrivateTermsNamesAListLineThatIsNotTheFileLine(t *testing.T) {
	list := aTermsDir(t, "other thing", termOne)
	repo := aRepoTracking(t, map[string][]byte{"a.txt": []byte("x\nx\nx\nx\nx\nx\nx\nzorblax\n")})
	// no baseline of this repo: an empty baseline allows 0 everywhere.
	if err := os.WriteFile(strings.TrimSuffix(list, ".txt")+".baseline", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := privateTerms(t, repo, list)
	if err == nil {
		t.Fatalf("expected a failure:\n%s", out)
	}
	if !strings.Contains(out, "a.txt:8") || !strings.Contains(out, "list line 4") {
		t.Fatalf("expected a.txt:8 and list line 4:\n%s", out)
	}
	noLeak(t, out, termOne)
}

func TestPrivateTermsPassesAtTheBaselineAndAllowsZeroForWhatItDoesNotName(t *testing.T) {
	list := aTermsDir(t, termOne, termTwo)
	repo := aRepoTracking(t, map[string][]byte{
		"a.md": []byte("zorblax one\nzorblax two\n"),
		"b.md": []byte("clean\n"),
	})
	baselineOf(t, repo, list)
	if out, err := privateTerms(t, repo, list); err != nil {
		t.Fatalf("expected a pass at the baseline, got %v:\n%s", err, out)
	}
	// a file the baseline does not name, holding a term it names for another file.
	if err := os.WriteFile(filepath.Join(repo, "b.md"), []byte("clean\nZORBLAX\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo)
	out, err := privateTerms(t, repo, list)
	if err == nil || !strings.Contains(out, "b.md") {
		t.Fatalf("expected a failure naming b.md, got %v:\n%s", err, out)
	}
	noLeak(t, out, termOne)

	// a term the baseline does not name at all.
	repo2 := aRepoTracking(t, map[string][]byte{"c.md": []byte("quenby   wick not a match\nQuenby wick, yes\n")})
	out, err = privateTerms(t, repo2, aTermsDirWithBaseline(t, termOne, termTwo))
	if err == nil || !strings.Contains(out, "c.md") {
		t.Fatalf("expected a failure naming c.md, got %v:\n%s", err, out)
	}
	noLeak(t, out, termTwo, "yes")
}

// aTermsDirWithBaseline is a list with an empty baseline beside it.
func aTermsDirWithBaseline(t *testing.T, terms ...string) string {
	t.Helper()
	list := aTermsDir(t, terms...)
	if err := os.WriteFile(strings.TrimSuffix(list, ".txt")+".baseline", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPrivateTermsMatchesWithoutCaseAndOnlyWhole(t *testing.T) {
	list := aTermsDirWithBaseline(t, "kip")
	hit := aRepoTracking(t, map[string][]byte{"a.txt": []byte("that was Kip's idea\n")})
	if out, err := privateTerms(t, hit, list); err == nil {
		t.Fatalf("expected a failure on Kip's:\n%s", out)
	}
	for _, line := range []string{"it was skipped\n", "skipping kippers\n", "kip2 and 9kip\n", "KIP\n", "(kip)\n", "kip_x\n"} {
		repo := aRepoTracking(t, map[string][]byte{"a.txt": []byte(line)})
		out, err := privateTerms(t, repo, list)
		want := strings.Contains(line, "KIP") || strings.Contains(line, "(kip)") || strings.Contains(line, "kip_x")
		if want && err == nil {
			t.Fatalf("expected %q to fail:\n%s", line, out)
		}
		if !want && err != nil {
			t.Fatalf("expected %q to pass, got %v:\n%s", line, err, out)
		}
	}
}

func TestPrivateTermsReadsNeitherAnUntrackedFileNorGit(t *testing.T) {
	list := aTermsDirWithBaseline(t, termOne)
	repo := aRepoTracking(t, map[string][]byte{"a.txt": []byte("clean\n")})
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("zorblax\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a commit message and a branch name hold the term; .git is not read.
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "about zorblax")
	gitIn(t, repo, "branch", "zorblax-branch")
	if out, err := privateTerms(t, repo, list); err != nil {
		t.Fatalf("expected a pass, got %v:\n%s", err, out)
	}
}

func TestPrivateTermsSkipsSayingWhichIsMissing(t *testing.T) {
	repo := aRepoTracking(t, map[string][]byte{"a.txt": []byte("zorblax\n")})
	dir := t.TempDir()

	out, err := privateTerms(t, repo, filepath.Join(dir, "private-terms.txt"))
	if err != nil || !strings.Contains(out, "skipped") || !strings.Contains(out, "list") {
		t.Fatalf("no list: expected a pass naming the list and skipped, got %v:\n%s", err, out)
	}

	list := aTermsDir(t, termOne)
	out, err = privateTerms(t, repo, list)
	if err != nil || !strings.Contains(out, "skipped") || !strings.Contains(out, "baseline") {
		t.Fatalf("no baseline: expected a pass naming the baseline and skipped, got %v:\n%s", err, out)
	}
	noLeak(t, out, termOne)
}

func TestPrivateTermsBaselinePrintsOneLinePerTermAndFileAndWritesNothing(t *testing.T) {
	list := aTermsDir(t, termOne, termTwo, "unused")
	repo := aRepoTracking(t, map[string][]byte{
		"a.md": []byte("zorblax\nzorblax again\nquenby wick\n"),
		"b.md": []byte("Quenby Wick\nnothing\n"),
	})
	before := dirListing(t, filepath.Dir(list))
	out, err := privateTerms(t, repo, list, "--baseline")
	if err != nil {
		t.Fatalf("--baseline: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (zorblax a.md, quenby a.md, quenby b.md), got %d:\n%s", len(lines), out)
	}
	for _, l := range lines {
		if !regexp.MustCompile(`^[0-9a-f]{12} [0-9]+ \S+\.md$`).MatchString(l) {
			t.Fatalf("a baseline line is not '<hash> <count> <path>': %q", l)
		}
	}
	if !strings.Contains(out, " 2 a.md") {
		t.Fatalf("expected a count of 2 for a.md:\n%s", out)
	}
	noLeak(t, out, termOne, termTwo)
	if after := dirListing(t, filepath.Dir(list)); after != before {
		t.Fatalf("--baseline changed the list's directory:\nbefore %s\nafter %s", before, after)
	}
	if st := gitIn(t, repo, "status", "--porcelain"); st != "" {
		t.Fatalf("--baseline changed the repository:\n%s", st)
	}
}

func dirListing(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

func TestPrivateTermsPassesThisRepository(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../scripts/check-private-terms.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := command(t, "sh", script, root)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("this repository holds a private term over the baseline: %v\n%s", err, out)
	}
}

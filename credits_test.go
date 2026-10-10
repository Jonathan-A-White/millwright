package millwright_test

// The README's Credits section opens with Newton's line and credits what the
// factory is built on. Adding a direct dependency to go.mod without crediting it
// there in the same commit fails here, and so does a Go-library credit for a
// module go.mod no longer requires, and a bundled font or data file no credit names.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// creditsSection is the README text from the "## Credits" heading to the next
// "## " heading.
func creditsSection(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	start := strings.Index(text, "\n## Credits\n")
	if start < 0 {
		t.Fatal(`README.md has no "## Credits" section`)
	}
	rest := text[start+len("\n## Credits\n"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// directRequires lists the module paths go.mod requires without "// indirect".
func directRequires(t *testing.T) []string {
	t.Helper()
	return requires(t, false)
}

// requires lists the module paths go.mod requires, with the "// indirect" ones
// only when withIndirect is set.
func requires(t *testing.T, withIndirect bool) []string {
	t.Helper()
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	var mods []string
	inBlock := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "require (":
			inBlock = true
			continue
		case inBlock && line == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !inBlock:
			continue
		}
		if line == "" || (!withIndirect && strings.Contains(line, "// indirect")) {
			continue
		}
		mods = append(mods, strings.Fields(line)[0])
	}
	return mods
}

func TestCreditsNameEveryDirectRequire(t *testing.T) {
	mods := directRequires(t)
	if len(mods) == 0 {
		t.Fatal("found no direct requires in go.mod; the parser is wrong")
	}
	credits := creditsSection(t)
	for _, mod := range mods {
		// A credit is a link to the module's page: [name](https://<module path>...).
		link := regexp.MustCompile(`\]\(https://` + regexp.QuoteMeta(mod) + `[/)#]`)
		if !link.MatchString(credits) {
			t.Errorf("go.mod requires %s, but README.md's Credits has no link to https://%s; credit it there", mod, mod)
		}
	}
}

func TestCreditsOpenWithNewton(t *testing.T) {
	credits := strings.TrimSpace(creditsSection(t))
	const line = "If I have seen further it is by standing on the shoulders of Giants."
	if !strings.HasPrefix(credits, "> "+line) {
		t.Fatalf("Credits must open with Newton's line as a quotation: %q", line)
	}
	first := strings.SplitN(credits, "\n\n", 2)[0]
	if !strings.Contains(first, "Newton") || !strings.Contains(first, "Hooke") || !strings.Contains(first, "1675") {
		t.Errorf("Newton's line must be attributed (Newton, to Robert Hooke, 1675); got %q", first)
	}
}

func TestCreditsLinkByNameNotByURL(t *testing.T) {
	credits := creditsSection(t)
	for _, m := range regexp.MustCompile(`\[([^\]]*)\]\(`).FindAllStringSubmatch(credits, -1) {
		if strings.Contains(m[1], "://") || strings.HasPrefix(m[1], "www.") {
			t.Errorf("link text %q is a raw URL; use the source's name", m[1])
		}
	}
	for _, bare := range regexp.MustCompile(`(^|[^(\[])https?://\S+`).FindAllString(credits, -1) {
		t.Errorf("bare URL %q in Credits; give the source's name as link text", strings.TrimSpace(bare))
	}
}

// goLibrariesSection is the text of the Credits' "### Go libraries" subsection,
// the one kind of credit that names a Go module.
func goLibrariesSection(credits string) string {
	const heading = "\n### Go libraries\n"
	credits = "\n" + credits
	start := strings.Index(credits, heading)
	if start < 0 {
		return ""
	}
	rest := credits[start+len(heading):]
	if end := strings.Index(rest, "\n#"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// staleLibraryCredits returns the targets (host and path) of the Go-library
// credits that name none of the modules in required. Only the "### Go libraries"
// subsection is read: a credit under another heading (an idea, a program on the
// path, a service) is not a Go module and is left alone.
func staleLibraryCredits(credits string, required []string) []string {
	var stale []string
	entry := regexp.MustCompile(`(?m)^- \[[^\]]*\]\(https://([^)\s]+)\)`)
	for _, m := range entry.FindAllStringSubmatch(goLibrariesSection(credits), -1) {
		target := strings.TrimRight(m[1], "/")
		named := false
		for _, mod := range required {
			// A module path may end in a major version (/v2) its repository page does not.
			mod = regexp.MustCompile(`/v[0-9]+$`).ReplaceAllString(mod, "")
			if target == mod || strings.HasPrefix(target, mod+"/") {
				named = true
			}
		}
		if !named {
			stale = append(stale, target)
		}
	}
	return stale
}

func TestCreditsNameNoModuleThatIsNotRequired(t *testing.T) {
	for _, stale := range staleLibraryCredits(creditsSection(t), requires(t, true)) {
		t.Errorf("README.md's Credits (Go libraries) names https://%s, but go.mod no longer requires it; remove the credit with the dependency", stale)
	}
}

func TestACreditForARemovedModuleIsStale(t *testing.T) {
	credits := "### Go libraries\n\n" +
		"- [cobra](https://github.com/spf13/cobra) (Apache 2.0): the command line.\n" +
		"- [godog](https://github.com/cucumber/godog) (MIT): the features.\n" +
		"  A second line of godog's entry, with a [link](https://example.com/not-an-entry).\n"
	stale := staleLibraryCredits(credits, []string{"github.com/spf13/cobra"})
	if len(stale) != 1 || stale[0] != "github.com/cucumber/godog" {
		t.Errorf("want only godog stale once its require is gone; got %v", stale)
	}
	if got := staleLibraryCredits(credits, []string{"github.com/spf13/cobra", "github.com/cucumber/godog/v2"}); len(got) != 0 {
		t.Errorf("a module path with a major-version suffix still credits its repository page; got stale %v", got)
	}
}

func TestACreditThatIsNotAModuleIsExemptByKind(t *testing.T) {
	credits := "### Tools millwright runs\n\n" +
		"- [Beads](https://github.com/steveyegge/beads) (MIT): the issue tracker.\n\n" +
		"### Go libraries\n\n" +
		"- [cobra](https://github.com/spf13/cobra) (Apache 2.0): the command line.\n\n" +
		"### Outside services and networks\n\n" +
		"- [WhatsOnChain](https://whatsonchain.com): a block explorer.\n"
	if stale := staleLibraryCredits(credits, []string{"github.com/spf13/cobra"}); len(stale) != 0 {
		t.Errorf("credits outside Go libraries name no module and must be left alone; got stale %v", stale)
	}
}

// bundledAssets lists the files under root that are fonts or data sources,
// outside test fixtures (testdata) and the .git directory.
func bundledAssets(t *testing.T, root string) []string {
	t.Helper()
	assetExt := map[string]bool{
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
		".csv": true, ".tsv": true, ".sqlite": true, ".sqlite3": true, ".db": true,
		".parquet": true, ".xlsx": true, ".dat": true,
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata" || d.Name() == "bin") {
			return filepath.SkipDir
		}
		if !d.IsDir() && assetExt[strings.ToLower(filepath.Ext(path))] {
			rel, _ := filepath.Rel(root, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// uncreditedAssets returns the assets whose file name the credits never mention.
func uncreditedAssets(credits string, assets []string) []string {
	var missing []string
	for _, a := range assets {
		if !strings.Contains(credits, filepath.Base(a)) {
			missing = append(missing, a)
		}
	}
	return missing
}

// millwright ships no font and no data file today (the embedded template is
// Markdown, and testdata holds only test fixtures); this test is the guard for
// the day one is added.
func TestEveryBundledFontAndDataFileIsCredited(t *testing.T) {
	for _, a := range uncreditedAssets(creditsSection(t), bundledAssets(t, ".")) {
		t.Errorf("%s is bundled but README.md's Credits never names it; credit its source in the same commit", a)
	}
}

func TestABundledFileNoCreditNamesIsReported(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"fonts/Inter.woff2", "data/words.csv", "testdata/fixture.csv", "notes.md"} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	assets := bundledAssets(t, dir)
	if len(assets) != 2 {
		t.Fatalf("want the font and the data file, not the fixture or the note; got %v", assets)
	}
	missing := uncreditedAssets("- [Inter](https://rsms.me/inter/): the font, Inter.woff2.", assets)
	if len(missing) != 1 || missing[0] != "data/words.csv" {
		t.Errorf("want only the uncredited data file reported; got %v", missing)
	}
}

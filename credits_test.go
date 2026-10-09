package millwright_test

// The README's Credits section opens with Newton's line and credits what the
// factory is built on. Adding a direct dependency to go.mod without crediting it
// there in the same commit fails here.

import (
	"os"
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
		if line == "" || strings.Contains(line, "// indirect") {
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

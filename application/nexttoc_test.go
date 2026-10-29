package application

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// tocStub is a branch that changes the files it lists and holds the documents
// it has, by path; a changed file with no document is one it deleted.
type tocStub struct {
	branchChangesStub
	docs    map[string]string
	readErr error
}

func (s tocStub) ReadBranchFile(_ context.Context, _, _, file string) ([]byte, bool, error) {
	if s.readErr != nil {
		return nil, false, s.readErr
	}
	text, ok := s.docs[file]
	return []byte(text), ok, nil
}

func tocRefusalOf(t *testing.T, changes BranchChanges) (Refusal, bool, *NextReport) {
	t.Helper()
	report := &NextReport{}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", branch: "mw/mw-t.1", target: "main"}
	refusal, refused := Next{Changes: changes}.tocRefusal(context.Background(), c, report)
	return refusal, refused, report
}

// sections is a document with n "## " headings and no table of contents.
func sections(n int) string {
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for i := 1; i <= n; i++ {
		b.WriteString("## Part " + string(rune('A'+i-1)) + "\n\ntext\n\n")
	}
	return b.String()
}

const staleToc = "# Title\n\n<!-- toc -->\n\n- [Old](#old)\n\n<!-- /toc -->\n\n## New\n\ntext\n"

func TestABranchThatChangesDocsWithAStaleOrMissingTableOfContentsIsRefusedOnceNamingThemAll(t *testing.T) {
	stub := tocStub{
		branchChangesStub: branchChangesStub{"docs/a.md", "docs/b.md", "docs/c.md", "CHANGELOG.md", "docs/d.md", "src/x.go"},
		docs: map[string]string{
			"docs/a.md":    staleToc,
			"docs/b.md":    sections(6),
			"docs/c.md":    sections(3),
			"CHANGELOG.md": staleToc,
		},
	}
	refusal, refused, _ := tocRefusalOf(t, stub)
	if !refused || refusal.Reason != ReasonTOCStale {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonTOCStale, refusal, refused)
	}
	for _, want := range []string{"2 doc(s) with a stale or missing table of contents: docs/a.md, docs/b.md", "Run: mw toc fix docs/a.md docs/b.md, then commit."} {
		if !strings.Contains(refusal.Said, want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, refusal.Said)
		}
	}
	for _, not := range []string{"docs/c.md", "CHANGELOG", "docs/d.md", "x.go"} {
		if strings.Contains(refusal.Said, not) {
			t.Errorf("expected the refusal not to name %q, got:\n%s", not, refusal.Said)
		}
	}
	if !strings.Contains(refusal.Why, "docs/a.md") {
		t.Errorf("expected the why to name the docs, got %q", refusal.Why)
	}
}

func TestABranchWhoseChangedDocsAreAllCurrentIsNotRefusedForATableOfContents(t *testing.T) {
	// A current table: what mw toc fix writes.
	current := "# Title\n\n<!-- toc -->\n\n- [Part A](#part-a)\n- [Part B](#part-b)\n- [Part C](#part-c)\n- [Part D](#part-d)\n\n<!-- /toc -->\n\n" +
		strings.TrimPrefix(sections(4), "# Title\n\n")
	stub := tocStub{
		branchChangesStub: branchChangesStub{"docs/a.md", "docs/c.md", "docs/d.md"},
		docs:              map[string]string{"docs/a.md": current, "docs/c.md": sections(3)},
	}
	if refusal, refused, _ := tocRefusalOf(t, stub); refused {
		t.Fatalf("expected no refusal, got %+v", refusal)
	}
}

func TestAChangedDocWithTheOptOutLineIsNeverRefusedForATableOfContents(t *testing.T) {
	stub := tocStub{
		branchChangesStub: branchChangesStub{"docs/a.md", "docs/b.md"},
		docs: map[string]string{
			"docs/a.md": "<!-- toc: none -->\n" + sections(9),
			"docs/b.md": strings.Replace(staleToc, "# Title\n", "# Title\n\n<!-- toc: none -->\n", 1),
		},
	}
	if refusal, refused, _ := tocRefusalOf(t, stub); refused {
		t.Fatalf("expected no refusal, got %+v", refusal)
	}
}

func TestATableOfContentsRefusalIsOnlyANetAndSaysNothingWhenTheBranchCannotBeRead(t *testing.T) {
	if _, refused, _ := tocRefusalOf(t, nil); refused {
		t.Errorf("expected no refusal with no Changes")
	}
	if _, refused, _ := tocRefusalOf(t, branchChangesStub{"docs/a.md"}); refused {
		t.Errorf("expected no refusal from a Changes that cannot read a file")
	}
	stub := tocStub{branchChangesStub: branchChangesStub{"docs/a.md"}, docs: map[string]string{"docs/a.md": staleToc}, readErr: errors.New("git is down")}
	refusal, refused, report := tocRefusalOf(t, stub)
	if refused {
		t.Errorf("expected no refusal when a file cannot be read, got %+v", refusal)
	}
	if len(report.Notes) == 0 {
		t.Errorf("expected a note that the file could not be read")
	}
}

package domain

import (
	"strings"
	"testing"
)

func TestTocAnchor(t *testing.T) {
	for title, want := range map[string]string{
		"A & B":            "a--b",
		"Plan / Notes":     "plan--notes",
		"Ends - here":      "ends---here",
		"my_var x":         "my_var-x",
		"  Padded  ":       "padded",
		"Ἕλληνες":          "ἕλληνες",
		"Step 2: go":       "step-2-go",
		"Hello, World!":    "hello-world",
		"The `mw toc` cmd": "the-mw-toc-cmd",
		"snake_case name":  "snake_case-name",
		"Already-hyphened": "already-hyphened",
	} {
		if got := TocAnchor(title); got != want {
			t.Errorf("TocAnchor(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestTocHeadingsAnchorsRepeatsAndNesting(t *testing.T) {
	doc := "# Title\n\n## Notes\n\n### Notes\n\n## Other\n\n### Sub\n\n## Notes\n\n#### Deep\n"
	got := Headings(doc)
	want := []Heading{
		{2, "Notes", "notes"},
		{3, "Notes", "notes-1"},
		{2, "Other", "other"},
		{3, "Sub", "sub"},
		{2, "Notes", "notes-2"},
	}
	if len(got) != len(want) {
		t.Fatalf("Headings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("heading %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTocHeadingsLeaveOutTheTitleAndFencedCode(t *testing.T) {
	doc := "# Title\n\n## Real\n\n```sh\n## not a heading\n```\n\n~~~\n### nor this\n~~~\n\n````\n```\n## still code\n````\n\n## After\n"
	got := Headings(doc)
	if len(got) != 2 || got[0].Title != "Real" || got[1].Title != "After" {
		t.Fatalf("Headings = %v, want Real and After only", got)
	}
}

func TestTocHeadingsDropAClosingSequence(t *testing.T) {
	got := Headings("## Closed ##\n## C#\n")
	if got[0].Title != "Closed" || got[1].Title != "C#" {
		t.Fatalf("Headings = %v", got)
	}
}

func TestTocRender(t *testing.T) {
	got := RenderToc(Headings("## A\n### B\n## C\n"))
	want := "- [A](#a)\n  - [B](#b)\n- [C](#c)\n"
	if got != want {
		t.Fatalf("RenderToc = %q, want %q", got, want)
	}
}

func longDoc(n int) string {
	var b strings.Builder
	b.WriteString("# Title\n\nIntro.\n\n")
	for i := 1; i <= n; i++ {
		b.WriteString("## Part " + strings.Repeat("x", i) + "\n\ntext\n\n")
	}
	return b.String()
}

func TestTocApplyInsertsBeforeTheFirstSectionAndIsIdempotent(t *testing.T) {
	doc := longDoc(4)
	once := ApplyToc(doc, 4)
	if !strings.Contains(once, "Intro.\n\n"+TocOpen+"\n\n- [Part x](#part-x)\n") ||
		!strings.Contains(once, "\n"+TocClose+"\n\n## Part x\n") {
		t.Fatalf("the table is not before the first ##:\n%s", once)
	}
	if twice := ApplyToc(once, 4); twice != once {
		t.Fatalf("applying twice changed the document:\n%s", twice)
	}
	if !TocCurrent(once) || TocCurrent(doc) {
		t.Fatalf("TocCurrent: once %v, bare %v", TocCurrent(once), TocCurrent(doc))
	}
}

func TestTocApplyKeepsABlankLineBeforeAHeadingThatFollowsText(t *testing.T) {
	got := ApplyToc("Intro\n## A\n## B\n", 2)
	if !strings.HasPrefix(got, "Intro\n\n"+TocOpen+"\n") {
		t.Fatalf("no blank line before the marker: %q", got)
	}
}

func TestTocApplyBelowThresholdOrOptedOutLeavesTheDocument(t *testing.T) {
	for name, doc := range map[string]string{
		"three":   longDoc(3),
		"opt out": longDoc(6) + TocNone + "\n",
	} {
		if got := ApplyToc(doc, 4); got != doc {
			t.Errorf("%s: document changed:\n%s", name, got)
		}
	}
	// An opt-out inside fenced code is only an example.
	if !TocWants("```\n"+TocNone+"\n```\n"+longDoc(4), 4) {
		t.Error("an opt-out line in fenced code opted the document out")
	}
}

func TestTocApplyRewritesBetweenMarkersWhateverTheCount(t *testing.T) {
	doc := "# T\n\n" + TocOpen + "\n- [old](#old)\n" + TocClose + "\n\n## Only\n\n### Sub\n\nafter\n"
	want := "# T\n\n" + TocOpen + "\n\n- [Only](#only)\n  - [Sub](#sub)\n\n" + TocClose + "\n\n## Only\n\n### Sub\n\nafter\n"
	if got := ApplyToc(doc, 4); got != want {
		t.Fatalf("ApplyToc = %q, want %q", got, want)
	}
	if TocStateOf(doc, 4) != TocStale || TocStateOf(want, 4) != TocFine {
		t.Fatalf("states: %v, %v", TocStateOf(doc, 4), TocStateOf(want, 4))
	}
}

func TestTocStateOf(t *testing.T) {
	if got := TocStateOf(longDoc(4), 4); got != TocMissing {
		t.Errorf("4 ## and no markers: %v, want missing", got)
	}
	if got := TocStateOf(longDoc(3), 4); got != TocFine {
		t.Errorf("3 ##: %v, want fine", got)
	}
	if got := TocStateOf(longDoc(6)+TocNone+"\n", 4); got != TocFine {
		t.Errorf("opted out: %v, want fine", got)
	}
}

func TestTocMarkersInFencedCodeAreNotMarkers(t *testing.T) {
	doc := "# T\n\n```\n" + TocOpen + "\n" + TocClose + "\n```\n\n## A\n## B\n"
	if got := TocStateOf(doc, 2); got != TocMissing {
		t.Fatalf("state = %v, want missing", got)
	}
}

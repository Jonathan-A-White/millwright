package application

import (
	"strings"
	"testing"
)

func TestSubjectOfTakesAPathBeforeABacktickedToken(t *testing.T) {
	for _, tc := range []struct{ sentence, want string }{
		{"Edit `make all` in app/parse.go first.", "app/parse.go"},
		{"Read docs/codemap.md.", "docs/codemap.md"},
		{"The file main.go is the entry.", "main.go"},
		{"Run `make build` then look in bin/.", "make build"},
		{"See https://example.org/a/b for why.", "general"},
		{"Nothing to anchor this to.", "general"},
		{"Use ~/postern/server/internal.", "~/postern/server/internal"},
	} {
		if got := subjectOf(tc.sentence); got != tc.want {
			t.Errorf("subjectOf(%q) = %q, want %q", tc.sentence, got, tc.want)
		}
	}
}

func TestSourceOfTakesTheLastBracketedBeadAndRemovesItsBracket(t *testing.T) {
	for _, tc := range []struct{ text, source, rest string }{
		{"A thing (mw-aa.1).", "mw-aa.1", "A thing."},
		{"A thing [mw-bb] and more.", "mw-bb", "A thing and more."},
		{"A thing (mw-aa) and (mw-bb.2.3).", "mw-bb.2.3", "A thing (mw-aa) and."},
		{"A thing (see the docs).", "", "A thing (see the docs)."},
		{"A thing mw-cc with no bracket.", "", "A thing mw-cc with no bracket."},
	} {
		source, rest := sourceOf(tc.text)
		if source != tc.source || rest != tc.rest {
			t.Errorf("sourceOf(%q) = %q, %q; want %q, %q", tc.text, source, rest, tc.source, tc.rest)
		}
	}
}

func TestAboutIsCutAtASentenceEndPast600Bytes(t *testing.T) {
	short := "Short head. Two sentences."
	if about, warning := aboutFrom("x.md", short); about != short || warning != "" {
		t.Errorf("a short head was changed: %q, %q", about, warning)
	}
	long := strings.Repeat("Ünï sentence. ", 60)
	about, warning := aboutFrom("x.md", strings.TrimSpace(long))
	if len(about) > AboutMaxBytes || !strings.HasSuffix(about, ".") || warning == "" {
		t.Errorf("a long head gave %d bytes %q, warning %q", len(about), about[len(about)-10:], warning)
	}
	noStop := strings.Repeat("é", 400)
	if about, _ := aboutFrom("x.md", noStop); len(about) != AboutMaxBytes {
		t.Errorf("a head with no sentence end gave %d bytes, want %d", len(about), AboutMaxBytes)
	}
}

func TestScanMemoryReadsHeadHeadingsAndStrayLines(t *testing.T) {
	text := "# Title\n\nHead one.\nStill head.\n\n## Decided here\n- Choice a.go (mw-1)\n\n## Other\n- Plain thing 2026-01-02.\n  continued\n- (mw-9)\n"
	got := scanMemory(text)
	if got.Head != "Head one.\nStill head." {
		t.Errorf("head = %q", got.Head)
	}
	if len(got.Lines) != 2 || got.Lines[0].Kind != FactDecision || got.Lines[1].Kind != FactGotcha {
		t.Fatalf("lines = %+v", got.Lines)
	}
	if got.Lines[0].Source != "mw-1" || got.Lines[0].Subject != "a.go" || got.Lines[1].Since != "2026-01-02" {
		t.Errorf("lines = %+v", got.Lines)
	}
	if len(got.NotPlaced) != 2 || !strings.HasPrefix(got.NotPlaced[0], "line 11: ") || !strings.HasPrefix(got.NotPlaced[1], "line 12: ") {
		t.Errorf("not placed = %q", got.NotPlaced)
	}
}

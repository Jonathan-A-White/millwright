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
		{"Run `make` then look in bin/.", "make"},
		{"Run `make build` then look in bin/.", "general"},
		{"See https://example.org/a/b for why.", "general"},
		{"Nothing to anchor this to.", "general"},
		{"Use ~/postern/server/internal.", "~/postern/server/internal"},
		// A placeholder path stays whole.
		{"every epic ends with a demo story (a docs/demo-<epic>.md he runs on his phone)", "docs/demo-<epic>.md"},
		{"Write <dir>/notes.md first.", "<dir>/notes.md"},
		{"Read demo-<epic>.md first.", "demo-<epic>.md"},
		// A path over the limit is passed over for the next one.
		{"Needs LD_LIBRARY_PATH=$HOME/.cache/ms-playwright-system-libs/usr/lib/x86_64-linux-gnu (tests/support/browser-env.ts sets it)", "tests/support/browser-env.ts"},
		{"Read " + strings.Repeat("a/", SubjectMaxChars) + "b.md only.", "b.md"},
		// A backticked token with a space, or over the limit, is not a subject.
		{"Run `npm ci --no-audit --no-fund && TZ=UTC npm test` before pushing.", "general"},
		{"Run `npm ci --no-audit --no-fund && cd src/bridge && npm test` first.", "src/bridge"},
		{"Run `" + strings.Repeat("x", SubjectMaxChars+1) + "` first.", "general"},
		{"Run `" + strings.Repeat("x", SubjectMaxChars) + "` first.", strings.Repeat("x", SubjectMaxChars)},
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
	if got.Lines[1].Sentence != "Plain thing 2026-01-02. continued" {
		t.Errorf("a continuation was not joined: %q", got.Lines[1].Sentence)
	}
	if len(got.NotPlaced) != 1 || !strings.HasPrefix(got.NotPlaced[0], "line 12: ") {
		t.Errorf("not placed = %q", got.NotPlaced)
	}
}

func TestScanMemoryJoinsEveryContinuationLineToItsBullet(t *testing.T) {
	text := "# Title\n\n## bd facts\n" +
		"- From inside a worktree `bd` finds no database: point every\n" +
		"  `bd` call at the vault with `-C`, as its OWN Bash call,\n" +
		"  never chained (a chained call is\n" +
		"  refused): `bd -C <vault> close <step> ...`; the vault\n" +
		"  has the one `.beads` (mw-gq6.52).\n" +
		"- Next bullet.\n"
	got := scanMemory(text)
	want := "From inside a worktree `bd` finds no database: point every `bd` call at the vault with `-C`, as its OWN Bash call, never chained (a chained call is refused): `bd -C <vault> close <step> ...`; the vault has the one `.beads`."
	if len(got.Lines) != 2 || got.Lines[0].Sentence != want {
		t.Fatalf("lines = %+v, want the first to read %q", got.Lines, want)
	}
	if got.Lines[0].Source != "mw-gq6.52" {
		t.Errorf("source = %q, want the bead id from the last continuation line", got.Lines[0].Source)
	}
	if len(got.NotPlaced) != 0 {
		t.Errorf("not placed = %q", got.NotPlaced)
	}
}

func TestScanMemoryTakesSubjectAndSinceFromTheJoinedText(t *testing.T) {
	got := scanMemory("# T\n\n- Wrapped so the path\n  is on line two: app/parse.go since 2026-03-04.\n")
	if len(got.Lines) != 1 || got.Lines[0].Subject != "app/parse.go" || got.Lines[0].Since != "2026-03-04" {
		t.Errorf("lines = %+v", got.Lines)
	}
}

func TestScanMemoryLeavesAnIndentedLineWithNoBulletAboveNotPlaced(t *testing.T) {
	// After a blank line or a heading, an indented line continues nothing.
	text := "# T\n\n- A bullet.\n\n  Indented after a blank.\n## H\n  Indented after a heading.\n"
	got := scanMemory(text)
	if len(got.Lines) != 1 || got.Lines[0].Sentence != "A bullet." {
		t.Errorf("lines = %+v", got.Lines)
	}
	if len(got.NotPlaced) != 2 {
		t.Errorf("not placed = %q", got.NotPlaced)
	}
}

package domain

import (
	"strconv"
	"strings"
	"unicode"
)

// TocOpen and TocClose are the lines a table of contents sits between, each on
// a line of its own.
const (
	TocOpen  = "<!-- toc -->"
	TocClose = "<!-- /toc -->"
	// TocNone, on a line of its own, leaves a document without a table of
	// contents whatever its headings.
	TocNone = "<!-- toc: none -->"
)

// TocMinHeadings is how many "## " headings make a document want a table of
// contents.
const TocMinHeadings = 4

// Heading is a "## " or "### " heading of a document and the anchor GitHub
// gives it.
type Heading struct {
	Level  int
	Title  string
	Anchor string
}

// TocState is how a document stands with its table of contents.
type TocState int

const (
	// TocFine: the table is current, or the document wants none.
	TocFine TocState = iota
	// TocStale: the document has markers and the list between them is not the
	// one its headings give.
	TocStale
	// TocMissing: the document wants a table and has no markers.
	TocMissing
)

// docLine is one line of a document and whether it is inside fenced code.
type docLine struct {
	text  string
	fence bool
}

// docLines splits doc into lines (the line break not kept) and marks those in
// a ``` or ~~~ fence, the fence lines themselves included.
func docLines(doc string) []docLine {
	parts := strings.Split(doc, "\n")
	lines := make([]docLine, len(parts))
	var marker byte
	var width int
	for i, part := range parts {
		line := strings.TrimRight(part, "\r")
		lines[i].text = line
		run, char := fenceRun(line)
		switch {
		case marker == 0 && run >= 3:
			marker, width = char, run
			lines[i].fence = true
		case marker != 0:
			lines[i].fence = true
			if char == marker && run >= width && strings.TrimSpace(line)[run:] == "" {
				marker = 0
			}
		}
	}
	return lines
}

// fenceRun is the length of the run of ` or ~ that opens line after at most
// three spaces, and which character it is; 0 when line starts no such run.
func fenceRun(line string) (int, byte) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == trimmed[0] {
		n++
	}
	return n, trimmed[0]
}

// headingOf is the level and title of a "## " or "### " line; 0 for any other.
func headingOf(line string) (int, string) {
	level := 0
	switch {
	case strings.HasPrefix(line, "### "):
		level = 3
	case strings.HasPrefix(line, "## "):
		level = 2
	default:
		return 0, ""
	}
	title := strings.TrimSpace(line[level+1:])
	if closed := strings.TrimRight(title, "#"); closed != title && (closed == "" || strings.HasSuffix(closed, " ")) {
		title = strings.TrimSpace(closed)
	}
	if title == "" {
		return 0, ""
	}
	return level, title
}

// TocAnchor is the anchor GitHub gives a heading's title: lower case, every
// rune that is not a letter, a number, an underscore or a hyphen dropped
// (a space kept), and the words joined with hyphens.
func TocAnchor(title string) string {
	var kept strings.Builder
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_' || r == '-' || unicode.IsSpace(r) {
			kept.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(kept.String()), "-")
}

// Headings are the "## " and "### " headings of doc, in order, with their
// anchors; a repeated anchor gets -1, -2 and so on. The "# " title and
// anything in fenced code are left out.
func Headings(doc string) []Heading {
	var out []Heading
	taken := map[string]bool{}
	repeats := map[string]int{}
	for _, line := range docLines(doc) {
		if line.fence {
			continue
		}
		level, title := headingOf(line.text)
		if level == 0 {
			continue
		}
		base := TocAnchor(title)
		anchor := base
		for taken[anchor] {
			repeats[base]++
			anchor = base + "-" + strconv.Itoa(repeats[base])
		}
		taken[anchor] = true
		out = append(out, Heading{Level: level, Title: title, Anchor: anchor})
	}
	return out
}

// RenderToc is the list of headings: "- [Title](#anchor)", a "###" indented
// two spaces. Every line ends in a line break; no headings gives "".
func RenderToc(headings []Heading) string {
	var b strings.Builder
	for _, h := range headings {
		if h.Level == 3 {
			b.WriteString("  ")
		}
		b.WriteString("- [" + h.Title + "](#" + h.Anchor + ")\n")
	}
	return b.String()
}

// tocBlock is the markers with the list between them, a blank line inside
// each: no line break at the end.
func tocBlock(headings []Heading) string {
	return TocOpen + "\n\n" + RenderToc(headings) + "\n" + TocClose
}

// tocSpan finds the marker lines outside fenced code: the index of the opening
// and of the closing line, -1 for both unless there is an opening followed by
// a closing.
func tocSpan(lines []docLine) (int, int) {
	open := -1
	for i, line := range lines {
		if line.fence {
			continue
		}
		switch strings.TrimSpace(line.text) {
		case TocOpen:
			if open < 0 {
				open = i
			}
		case TocClose:
			if open >= 0 {
				return open, i
			}
		}
	}
	return -1, -1
}

// TocOptedOut is whether doc holds the line TocNone, outside fenced code.
func TocOptedOut(doc string) bool {
	for _, line := range docLines(doc) {
		if !line.fence && strings.TrimSpace(line.text) == TocNone {
			return true
		}
	}
	return false
}

// TocWants is whether doc should have a table of contents: at least min "## "
// headings and no opt-out line.
func TocWants(doc string, min int) bool {
	if TocOptedOut(doc) {
		return false
	}
	count := 0
	for _, h := range Headings(doc) {
		if h.Level == 2 {
			count++
		}
	}
	return count >= min
}

// TocCurrent is whether doc has markers with exactly the list its headings give
// between them.
func TocCurrent(doc string) bool {
	if open, _ := tocSpan(docLines(doc)); open < 0 {
		return false
	}
	return ApplyToc(doc, 0) == doc
}

// TocStateOf says how doc stands: a document with an opt-out line is fine
// whatever it holds; one with markers is stale unless their list is current;
// one without them is missing a table when it wants one.
func TocStateOf(doc string, min int) TocState {
	if TocOptedOut(doc) {
		return TocFine
	}
	if open, _ := tocSpan(docLines(doc)); open >= 0 {
		if TocCurrent(doc) {
			return TocFine
		}
		return TocStale
	}
	if TocWants(doc, min) {
		return TocMissing
	}
	return TocFine
}

// ApplyToc is doc with its table of contents current: the list between its
// markers rewritten, or, when it has none and wants a table (min or more "## "
// headings), the markers and list put before the first "## ". A document with
// an opt-out line, or with neither markers nor enough headings, comes back as
// it was, and applying twice changes nothing more.
func ApplyToc(doc string, min int) string {
	if TocOptedOut(doc) {
		return doc
	}
	lines := docLines(doc)
	raw := strings.Split(doc, "\n")
	block := tocBlock(Headings(doc))

	if open, closeAt := tocSpan(lines); open >= 0 {
		out := append(append([]string{}, raw[:open]...), strings.Split(block, "\n")...)
		return strings.Join(append(out, raw[closeAt+1:]...), "\n")
	}
	if !TocWants(doc, min) {
		return doc
	}
	for i, line := range lines {
		if line.fence {
			continue
		}
		if level, _ := headingOf(line.text); level == 2 {
			out := append([]string{}, raw[:i]...)
			if i > 0 && strings.TrimSpace(raw[i-1]) != "" {
				out = append(out, "")
			}
			out = append(append(out, strings.Split(block, "\n")...), "")
			return strings.Join(append(out, raw[i:]...), "\n")
		}
	}
	return doc
}

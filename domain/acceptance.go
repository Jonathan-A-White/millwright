package domain

import (
	"regexp"
	"strings"
)

// nextItem is where the next numbered item of a run-on line begins: the end of
// the item before it.
var nextItem = regexp.MustCompile(`\s\d+[.)]\s`)

// LinesBeginning reads text, a story's acceptance criteria or description, for
// the lines that begin with word and a colon, after an optional bullet or list
// number of the line's own: "3. Sweep: ...", "- Expect: ..." (the bullet is
// trimmed first, by TrimListMarker; a number may go without its stop). A
// numbered item run on in a line after other items, as "... 4. It exits
// non-zero. 5. Sweep: ...", is read too, up to the next numbered item
// (mw-6drexa.1, mw-gq6.469); without its number the word mid-line is prose, not
// an item. It returns what follows the colon on each, trimmed, in order. Every
// convention a story's acceptance is read for (Sweep:, Expect:) is read by it,
// so a new required line is one more call.
func LinesBeginning(text, word string) []string {
	w := regexp.QuoteMeta(word)
	line := regexp.MustCompile(`(?i)^(?:\d+\s*[.)]?\s*)?` + w + `:(.*)$`)
	item := regexp.MustCompile(`(?i)\s\d+[.)]\s+` + w + `:(.*)$`)
	var found []string
	for _, l := range strings.Split(text, "\n") {
		m := line.FindStringSubmatch(TrimListMarker(l))
		if m == nil {
			m = item.FindStringSubmatch(l)
		}
		if m == nil {
			continue
		}
		got := m[1]
		if next := nextItem.FindStringIndex(got); next != nil {
			got = got[:next[0]]
		}
		found = append(found, strings.TrimSpace(got))
	}
	return found
}

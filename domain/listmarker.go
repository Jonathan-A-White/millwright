package domain

import (
	"regexp"
	"strings"
)

// listMarker is the bullet or item number a line of a list may start with:
// "- ", "* ", "+ ", "• ", "5. " or "5) ", always with a space after it.
var listMarker = regexp.MustCompile(`^\s*(?:[-*+•]|\d+[.)])\s+`)

// TrimListMarker is line without its leading space and one bullet or item
// number, so that a line mw reads by how it starts ("Sweep:", "What's new:",
// "Demo:") is read the same in a list the Mayor or a Builder numbered
// (mw-gq6.469).
func TrimListMarker(line string) string {
	return strings.TrimLeft(listMarker.ReplaceAllString(line, ""), " \t")
}

package application

import (
	"strings"
	"testing"
)

func TestLandingCheckIsTheFirstSentenceAfterTheMarker(t *testing.T) {
	cases := []struct {
		name     string
		comments []string
		want     string
	}{
		{"none", []string{"a word", "another"}, ""},
		{"colon and full stop dropped", []string{"Landing checked: gate green on main. Then more."}, "gate green on main"},
		{"a line ends it", []string{"Landing checked - all good\nsecond line"}, "all good"},
		{"newest carrying the marker wins", []string{"Landing checked: old.", "chatter", "Landing checked: new."}, "new"},
		{"the marker alone says nothing", []string{"Landing checked."}, ""},
		{"a dotted version does not end it", []string{"Landing checked: bd 1.3.0 answers. Done."}, "bd 1.3.0 answers"},
	}
	for _, c := range cases {
		var comments []Comment
		for _, text := range c.comments {
			comments = append(comments, Comment{Text: text})
		}
		if got := landingCheck(comments); got != c.want {
			t.Errorf("%s: expected %q, got %q", c.name, c.want, got)
		}
	}
}

func TestHowToCheckIsTheSectionAfterTheMarker(t *testing.T) {
	long := strings.Repeat("x", viewHowToLimit+50)
	cases := []struct {
		name     string
		comments []string
		want     string
	}{
		{"none", []string{"Internal: nothing for the Governor to look at."}, ""},
		{"the marker alone says nothing", []string{"HOW TO CHECK IT"}, ""},
		{"for the Governor is part of the heading", []string{"Done.\nHOW TO CHECK IT, for the Governor:\n1. Open it.\n2. See it."}, "1. Open it.\n2. See it."},
		{"the rig memory line ends it", []string{"HOW TO CHECK IT: 1. Open it.\nFor the rig memory: nothing"}, "1. Open it."},
		{"a Markdown heading ends it", []string{"HOW TO CHECK IT\n1. Open it.\n\n## Left undone\nnothing"}, "1. Open it."},
		{"a line of capitals ends it", []string{"HOW TO CHECK IT\n1. Open it.\nWHAT IS LEFT\nnothing"}, "1. Open it."},
		{"an image link stays", []string{"HOW TO CHECK IT\n1. Look: ![the card](https://example.test/card.png)"}, "1. Look: ![the card](https://example.test/card.png)"},
		{"newest carrying the marker wins", []string{"HOW TO CHECK IT: old", "chatter", "HOW TO CHECK IT: new"}, "new"},
		{"cut to the limit", []string{"HOW TO CHECK IT: " + long}, strings.Repeat("x", viewHowToLimit) + "…"},
	}
	for _, c := range cases {
		var comments []Comment
		for _, text := range c.comments {
			comments = append(comments, Comment{Text: text})
		}
		if got := howToCheck(comments); got != c.want {
			t.Errorf("%s: expected %q, got %q", c.name, c.want, got)
		}
	}
}

func TestLandedHowToFindsTheSectionInTheMemoryOfAnOlderRun(t *testing.T) {
	mem := posternSnapshotMemoryEntry{Comments: []PosternSnapshotComment{{Text: "newest"}, {Text: "HOW TO CHECK IT: 1. Look."}}}
	if got := landedHowTo(mem); got != "1. Look." {
		t.Errorf("expected the section from the remembered comments, got %q", got)
	}
	mem.HowTo = "kept whole"
	if got := landedHowTo(mem); got != "kept whole" {
		t.Errorf("expected the kept section, got %q", got)
	}
}

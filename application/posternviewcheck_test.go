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
		{"newest carrying the marker wins", []string{"HOW TO CHECK IT: 1. old", "chatter", "HOW TO CHECK IT: 1. new"}, "1. new"},
		{"a later mention is not the steps", []string{"HOW TO CHECK IT, for the Governor:\n1. Open it.\n2. See it.", "HOW TO CHECK IT is in the closing comment)."}, "1. Open it.\n2. See it."},
		{"a mention alone says nothing", []string{"HOW TO CHECK IT is in the closing comment)."}, ""},
		{"the Internal line is a section", []string{"HOW TO CHECK IT: Internal: nothing for the Governor to look at."}, "Internal: nothing for the Governor to look at."},
		{"cut to the limit", []string{"HOW TO CHECK IT: 1. " + long}, "1. " + strings.Repeat("x", viewHowToLimit-3) + "…"},
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

func TestHowToCheckCountsTheRealHeadingsWithQualifiers(t *testing.T) {
	steps := "1. Open Postern.\n2. See it."
	headings := map[string]string{
		"mw-j0f2d.14":  "HOW TO CHECK IT (for the Governor)\n\n",
		"mw-gq6.206":   "HOW TO CHECK IT (for the Governor): ",
		"mw-j0f2d.29":  "HOW TO CHECK IT (for the Governor, on his phone, after the build is live): ",
		"mw-j0f2d.38":  "HOW TO CHECK IT, for the Governor (after the backend is swapped and the new site is live):\n\n",
		"mw-nqur1n.11": "HOW TO CHECK IT, for the Governor (after the backend is swapped and the new\nsite is live):\n\n",
		"mw-a0ih0.12":  "HOW TO CHECK IT (for the Governor):\n\n",
	}
	for id, heading := range headings {
		got := howToCheck([]Comment{{Text: "Done.\n" + heading + steps + "\nFor the rig memory: nothing"}})
		if got != steps {
			t.Errorf("%s: expected the steps, got %q", id, got)
		}
	}
	// mw-j0f2d.37: the heading is ", for the Governor" alone on its line, no colon, no parenthesis.
	talk := "Done.\nHOW TO CHECK IT, for the Governor\n\n1. Reload Postern on the phone and open the Talk line (Channels, then Talk\nto the Mayor).\n2. Press and hold 'Hold to talk'. It reads 'Release to send'.\n\nFor the rig memory: nothing"
	if got, want := howToCheck([]Comment{{Text: talk}}), "1. Reload Postern on the phone and open the Talk line (Channels, then Talk\nto the Mayor).\n2. Press and hold 'Hold to talk'. It reads 'Release to send'."; got != want {
		t.Errorf("a heading with no colon must count, got %q", got)
	}
	if got := howToCheck([]Comment{{Text: "HOW TO CHECK IT, for the Governor\n\nInternal: nothing for the Governor to look at."}}); got != "Internal: nothing for the Governor to look at." {
		t.Errorf("a colonless heading before the Internal line must count, got %q", got)
	}
	if got := howToCheck([]Comment{{Text: "HOW TO CHECK IT, for the Governor\n\nNothing yet.\n1. Not this."}}); got != "" {
		t.Errorf("a colonless heading must be followed at once by steps, got %q", got)
	}
	if got := howToCheck([]Comment{{Text: "HOW TO CHECK IT (for the Governor, on his phone):\n\nInternal: nothing for the Governor to look at."}}); got != "Internal: nothing for the Governor to look at." {
		t.Errorf("expected the Internal line, got %q", got)
	}
	if got := howToCheck([]Comment{{Text: "HOW TO CHECK IT is in the closing comment).\n1. Not this."}}); got != "" {
		t.Errorf("a mention is not the steps, got %q", got)
	}
	if got := howToCheck([]Comment{{Text: "HOW TO CHECK IT\n1. Open it: then see it."}}); got != "1. Open it: then see it." {
		t.Errorf("a colon inside the first step must stay, got %q", got)
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
